// Package stub — детерминированные заглушки ИИ для локальной разработки и
// работы без HF_TOKEN. Все результаты явно помечаются Engine="stub".
package stub

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strings"

	"memorybutton/backend/internal/ai"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "stub" }

// ---------- ASR ----------

func (p *Provider) Transcribe(_ context.Context, audio []byte, filename, _ string) (*ai.ASRResult, error) {
	name := filename
	if name == "" {
		name = "audio"
	}
	return &ai.ASRResult{
		Text:   fmt.Sprintf("[stub-asr] распознавание недоступно без HF_TOKEN; аудио %s, %d байт", name, len(audio)),
		Engine: "stub",
	}, nil
}

// ---------- LLM ----------

var interviewQuestions = []string{
	"Расскажите, что вы помните об этом человеке? Начните с имени и приблизительных дат.",
	"Где он(а) родился(ась) и жил(а)? Есть ли связанные с этим истории?",
	"Кем он(а) работал(а)? Были ли важные для семьи события?",
	"Как вы его(её) запомнили? Какие черты характера или привычки?",
	"Есть ли фотографии или документы, которые можно было бы добавить?",
}

func (p *Provider) Chat(_ context.Context, req ai.ChatRequest) (*ai.ChatResult, error) {
	userCount := 0
	lastUser := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			userCount++
			lastUser = m.Content
		}
	}

	switch req.Purpose {
	case ai.PurposeInterview:
		q := interviewQuestions[(userCount-1)%len(interviewQuestions)]
		if userCount == 0 {
			q = "Чем вы хотите поделиться сегодня?"
		}
		return &ai.ChatResult{Text: q, Model: "stub"}, nil

	case ai.PurposeExtract:
		// Никакой выдумки: возвращаем пустые массивы и один уточняющий вопрос.
		payload := map[string]any{
			"people":       []any{},
			"relationships": []any{},
			"facts":        []any{},
			"questions": []map[string]any{
				{"text": "Что ещё важно запомнить об этом человеке?", "reason": "stub-extractor"},
			},
			"story":      "",
			"transcript": lastUser,
		}
		b, _ := json.Marshal(payload)
		return &ai.ChatResult{Text: string(b), Model: "stub"}, nil

	case ai.PurposeSummarize:
		s := lastUser
		if len(s) > 300 {
			s = s[:300] + "…"
		}
		if s == "" {
			s = "(пока пусто)"
		}
		return &ai.ChatResult{Text: "Краткая сводка: " + s, Model: "stub"}, nil

	case ai.PurposeQA:
		s := lastUser
		if len(s) > 600 {
			s = s[:600] + "…"
		}
		return &ai.ChatResult{Text: "Согласно сохранённым данным: " + s, Model: "stub"}, nil

	default:
		if lastUser == "" {
			lastUser = "(пустой ввод)"
		}
		return &ai.ChatResult{Text: "[stub-llm] " + lastUser, Model: "stub"}, nil
	}
}

// ---------- TTS ----------

// Synthesize tries espeak-ng for real Russian speech; falls back to a short beep WAV.
func (p *Provider) Synthesize(ctx context.Context, text string) (*ai.TTSResult, error) {
	if out, err := synthesizeEspeak(ctx, text); err == nil && len(out) > 44 {
		return &ai.TTSResult{Audio: out, Format: "wav", Engine: "espeak-ng"}, nil
	}
	// Fallback: short valid WAV beep so client can still play something.
	sampleRate := 16000
	duration := 0.25
	if len(text) > 100 {
		duration = 0.5
	}
	n := int(float64(sampleRate) * duration)
	var pcm []byte
	buf := make([]byte, 2)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		env := math.Min(1, 10*t) * math.Exp(-3*t)
		v := int16(8000 * env * math.Sin(2*math.Pi*440*t))
		binary.LittleEndian.PutUint16(buf, uint16(v))
		pcm = append(pcm, buf...)
	}
	header := wavHeader(len(pcm), sampleRate, 1, 16)
	return &ai.TTSResult{Audio: append(header, pcm...), Format: "wav", Engine: "stub-beep"}, nil
}

func synthesizeEspeak(ctx context.Context, text string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Пустой ответ"
	}
	// Limit length to avoid very long synthesis in turn-by-turn flow.
	runes := []rune(text)
	if len(runes) > 800 {
		text = string(runes[:800])
	}
	cmd := exec.CommandContext(ctx, "espeak-ng", "-v", "ru", "-s", "160", "--stdout", text)
	return cmd.Output()
}

func wavHeader(dataLen, sampleRate, channels, bits int) []byte {
	var b strings.Builder
	b.WriteString("RIFF")
	writeU32(&b, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	writeU32(&b, 16)
	writeU16(&b, 1) // PCM
	writeU16(&b, uint16(channels))
	writeU32(&b, uint32(sampleRate))
	writeU32(&b, uint32(sampleRate*channels*bits/8))
	writeU16(&b, uint16(channels*bits/8))
	writeU16(&b, uint16(bits))
	b.WriteString("data")
	writeU32(&b, uint32(dataLen))
	return []byte(b.String())
}

func writeU32(b *strings.Builder, v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}

func writeU16(b *strings.Builder, v uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	b.Write(buf[:])
}
