package ai

import (
	"context"
	"fmt"
)

// Обёртки graceful fallback: если основной провайдер недоступен, результат
// заглушки помечается Fallback=true, и клиент видит это в ответе.

type ASRFallback struct {
	Primary  ASR
	Fallback ASR
}

func (f ASRFallback) Transcribe(ctx context.Context, audio []byte, filename, language string) (*ASRResult, error) {
	res, err := f.Primary.Transcribe(ctx, audio, filename, language)
	if err == nil {
		return res, nil
	}
	if f.Fallback == nil {
		return nil, err
	}
	res2, err2 := f.Fallback.Transcribe(ctx, audio, filename, language)
	if err2 != nil {
		return nil, fmt.Errorf("asr primary: %v; fallback: %w", err, err2)
	}
	res2.Fallback = true
	return res2, nil
}

type LLMFallback struct {
	Primary  LLM
	Fallback LLM
}

func (f LLMFallback) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	res, err := f.Primary.Chat(ctx, req)
	if err == nil {
		return res, nil
	}
	if f.Fallback == nil {
		return nil, err
	}
	res2, err2 := f.Fallback.Chat(ctx, req)
	if err2 != nil {
		return nil, fmt.Errorf("llm primary: %v; fallback: %w", err, err2)
	}
	res2.Fallback = true
	return res2, nil
}

type TTSFallback struct {
	Primary  TTS
	Fallback TTS
}

func (f TTSFallback) Synthesize(ctx context.Context, text string) (*TTSResult, error) {
	res, err := f.Primary.Synthesize(ctx, text)
	if err == nil {
		return res, nil
	}
	if f.Fallback == nil {
		return nil, err
	}
	res2, err2 := f.Fallback.Synthesize(ctx, text)
	if err2 != nil {
		return nil, fmt.Errorf("tts primary: %v; fallback: %w", err, err2)
	}
	res2.Fallback = true
	return res2, nil
}
