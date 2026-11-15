package providerfactory

import (
	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/ai/hf"
	"memorybutton/backend/internal/ai/stub"
	"memorybutton/backend/internal/config"
)

// BuildProvider собирает провайдера ИИ из конфигурации.
//
//   - AI_PROVIDER=stub: всегда заглушки (локальная разработка).
//   - AI_PROVIDER=hf:   Hugging Face; при AI_FALLBACK_TO_STUB=true (по умолчанию)
//     любой сбой HF подменяется заглушкой, а результат помечается Fallback=true.
func Build(cfg config.AIConfig) ai.Provider {
	stubProvider := stub.New()

	if cfg.Provider == "hf" {
		hfClient := hf.NewClient(cfg)
		p := ai.Provider{Name: "hf", ASR: hfClient, LLM: hfClient, TTS: hfClient}
		if cfg.FallbackToStub {
			p.Name = "hf+stub-fallback"
			p.ASR = ai.ASRFallback{Primary: hfClient, Fallback: stubProvider}
			p.LLM = ai.LLMFallback{Primary: hfClient, Fallback: stubProvider}
			p.TTS = ai.TTSFallback{Primary: hfClient, Fallback: stubProvider}
		}
		return p
	}
	return ai.Provider{Name: "stub", ASR: stubProvider, LLM: stubProvider, TTS: stubProvider}
}
