// Package ai определяет интерфейсы к ИИ-сервисам (ASR/LLM/TTS) и их реализации.
package ai

import (
	"context"
	"time"
)

type Segment struct {
	Start   float64 `json:"start,omitempty"`
	End     float64 `json:"end,omitempty"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker,omitempty"`
}

// ---------- ASR ----------

type ASRResult struct {
	Text     string    `json:"text"`
	Segments []Segment `json:"segments,omitempty"`
	Engine   string    `json:"engine"`
	Fallback bool      `json:"fallback"`
}

type ASR interface {
	Transcribe(ctx context.Context, audio []byte, filename, language string) (*ASRResult, error)
}

// ---------- LLM ----------

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Purpose подсказывает заглушке, какую роль играет запрос. HF-реализация игнорирует поле.
type ChatPurpose string

const (
	PurposeInterview ChatPurpose = "interview"
	PurposeExtract   ChatPurpose = "extract"
	PurposeSummarize ChatPurpose = "summarize"
	PurposeQA        ChatPurpose = "qa"
	PurposeGeneric   ChatPurpose = "generic"
)

type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	Purpose     ChatPurpose
	JSONMode    bool
	Temperature float64
	MaxTokens   int
}

type ChatResult struct {
	Text     string
	Model    string
	Fallback bool
}

type LLM interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResult, error)
}

// ---------- TTS ----------

type TTSResult struct {
	Audio    []byte
	Format   string // wav, mp3, ogg...
	Engine   string
	Fallback bool
}

type TTS interface {
	Synthesize(ctx context.Context, text string) (*TTSResult, error)
}

// ---------- Провайдер ----------

type Provider struct {
	Name string
	ASR  ASR
	LLM  LLM
	TTS  TTS
}

// Timeout по умолчанию для внешних вызовов.
const DefaultTimeout = 90 * time.Second
