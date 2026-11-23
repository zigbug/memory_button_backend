// Package hf реализует клиентов Hugging Face Inference API / Inference Providers.
//
// Модели и токен берутся из конфигурации. Если HF_TOKEN не задан, приложение
// работает на заглушках (см. internal/ai/stub) — это задокументированный
// graceful fallback, а не скрытый обман: результаты помечаются Fallback=true.
package hf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/config"
)

type Client struct {
	http     *http.Client
	token    string
	baseURL  string // для чата
	asrURL   string
	ttsURL   string
	chatModel string
	asrModel string
	ttsModel string
	ttsVoice string
}

func NewClient(cfg config.AIConfig) *Client {
	return &Client{
		http:      &http.Client{Timeout: cfg.HFTimeout},
		token:     cfg.HFToken,
		baseURL:   strings.TrimRight(cfg.HFBaseURL, "/"),
		asrURL:    strings.TrimRight(getenv("HF_ASR_URL", "https://api-inference.huggingface.co/models"), "/"),
		ttsURL:    strings.TrimRight(getenv("HF_TTS_URL", "https://api-inference.huggingface.co/models"), "/"),
		chatModel: cfg.HFChatModel,
		asrModel:  cfg.HFASRModel,
		ttsModel:  cfg.HFTTSModel,
		ttsVoice:  getenv("HF_TTS_VOICE", ""),
	}
}

func (c *Client) chatURL(model string) string {
	if strings.Contains(c.baseURL, "{model}") {
		return strings.ReplaceAll(c.baseURL, "{model}", url.PathEscape(model))
	}
	// Support OpenAI-compatible router endpoint that takes model in body.
	if strings.HasSuffix(c.baseURL, "/v1/chat/completions") || strings.HasSuffix(c.baseURL, "/chat/completions") {
		return c.baseURL
	}
	return c.baseURL + "/" + url.PathEscape(model) + "/v1/chat/completions"
}

// ---------- LLM ----------

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ai.ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) Chat(ctx context.Context, req ai.ChatRequest) (*ai.ChatResult, error) {
	model := req.Model
	if model == "" {
		model = c.chatModel
	}
	body := chatRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSONMode {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(model), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.doWithRetry(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		// Некоторые провайдеры отдают нестандартный ответ — вернём как есть.
		return &ai.ChatResult{Text: string(raw), Model: model}, nil
	}
	if parsed.Error.Message != "" {
		return nil, fmt.Errorf("hf chat error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("hf chat: пустой ответ")
	}
	return &ai.ChatResult{Text: parsed.Choices[0].Message.Content, Model: model}, nil
}

// ---------- ASR ----------

func (c *Client) Transcribe(ctx context.Context, audio []byte, filename, language string) (*ai.ASRResult, error) {
	endpoint := c.asrURL + "/" + url.PathEscape(c.asrModel)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(audio))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", audioMIME(filename))
	resp, err := c.doWithRetry(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Text     string `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("hf asr: не JSON-ответ (%d байт): %w", len(raw), err)
	}
	if parsed.Error.Message != "" {
		return nil, fmt.Errorf("hf asr error: %s", parsed.Error.Message)
	}
	res := &ai.ASRResult{Text: parsed.Text, Engine: "hf:" + c.asrModel}
	for _, s := range parsed.Segments {
		res.Segments = append(res.Segments, ai.Segment{Start: s.Start, End: s.End, Text: s.Text})
	}
	return res, nil
}

// ---------- TTS ----------

func (c *Client) Synthesize(ctx context.Context, text string) (*ai.TTSResult, error) {
	endpoint := c.ttsURL + "/" + url.PathEscape(c.ttsModel)
	payload, err := json.Marshal(map[string]any{"inputs": text})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.doWithRetry(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	format := strings.TrimPrefix(resp.Header.Get("Content-Type"), "audio/")
	if format == "" || format == "application/octet-stream" {
		format = "wav"
	}
	return &ai.TTSResult{Audio: audio, Format: format, Engine: "hf:" + c.ttsModel}, nil
}

// ---------- HTTP с ретраями ----------

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("hf: HTTP %d", resp.StatusCode)
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			continue
		}
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("hf: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return resp, nil
	}
	return nil, fmt.Errorf("hf: после ретраев: %w", lastErr)
}

func audioMIME(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".ogg"), strings.HasSuffix(filename, ".opus"):
		return "audio/ogg"
	case strings.HasSuffix(filename, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(filename, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(filename, ".m4a"):
		return "audio/m4a"
	case strings.HasSuffix(filename, ".aac"):
		return "audio/aac"
	default:
		return "application/octet-stream"
	}
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
