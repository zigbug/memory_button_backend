// Package pipeline реализует диалоговый оркестратор и постобработку сессий.
package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/domain"
	"memorybutton/backend/internal/storage/media"
	"memorybutton/backend/internal/storage/postgres"
)

var ErrSessionNotActive = postgres.ErrSessionNotActive

type Dialogue struct {
	store  *postgres.Store
	ai     ai.Provider
	media  *media.Service
	logger *slog.Logger
}

func NewDialogue(store *postgres.Store, provider ai.Provider, mediaSvc *media.Service, logger *slog.Logger) *Dialogue {
	return &Dialogue{store: store, ai: provider, media: mediaSvc, logger: logger}
}

type TurnResult struct {
	Session        domain.Session `json:"session"`
	UserTurn       domain.Turn    `json:"user_turn"`
	AssistantTurn  domain.Turn    `json:"assistant_turn"`
}

// StartSession создаёт сессию и первую реплику помощника (приветствие+контекст).
func (d *Dialogue) StartSession(ctx context.Context, mode domain.SessionMode, personID string, questionIDs []string) (*domain.Session, *domain.Turn, error) {
	switch mode {
	case domain.ModeNewStory, domain.ModeContinuePerson, domain.ModeClarifyPerson, domain.ModeQA:
	default:
		return nil, nil, fmt.Errorf("неизвестный режим сессии: %q", mode)
	}
	if (mode == domain.ModeContinuePerson || mode == domain.ModeClarifyPerson) && personID == "" {
		return nil, nil, fmt.Errorf("для режима %s нужен person_id", mode)
	}
	if personID != "" {
		if _, err := d.store.GetPerson(ctx, personID); err != nil {
			return nil, nil, fmt.Errorf("person: %w", err)
		}
	}
	sess := &domain.Session{Mode: mode, Status: domain.SessionActive, QuestionIDs: questionIDs}
	if personID != "" {
		sess.PersonID = &personID
	}
	if err := d.store.CreateSession(ctx, sess); err != nil {
		return nil, nil, err
	}

	greeting := d.greeting(ctx, sess)
	turn := &domain.Turn{SessionID: sess.ID, Seq: 1, Role: domain.RoleAssistant, Text: greeting, Engine: "orchestrator"}
	if audio, err := d.ai.TTS.Synthesize(ctx, greeting); err == nil {
		m, err := d.media.Upload(ctx, domain.MediaAudio, "session", sess.ID, "greeting."+audio.Format, audio.Audio, "audio/"+audio.Format)
		if err == nil {
			turn.AudioMediaID = &m.ID
			turn.Engine = audio.Engine
			if audio.Fallback {
				turn.Fallback = true
			}
		} else {
			d.logger.Warn("не удалось сохранить приветствие TTS", "err", err)
		}
	} else {
		d.logger.Warn("TTS приветствия недоступен", "err", err)
	}
	if err := d.store.AddTurn(ctx, turn); err != nil {
		return nil, nil, err
	}
	if turn.AudioMediaID != nil {
		if m, err := d.store.GetMedia(ctx, *turn.AudioMediaID); err == nil {
			turn.AudioURL = m.URL
		}
	}
	d.logger.Info("сессия создана", "session_id", sess.ID, "mode", mode)
	return sess, turn, nil
}

func (d *Dialogue) greeting(ctx context.Context, sess *domain.Session) string {
	switch sess.Mode {
	case domain.ModeNewStory:
		return "Здравствуйте! Готовы записать новый рассказ. С кого начнём — назовите имя и приблизительные годы жизни."
	case domain.ModeQA:
		return "Задайте вопрос — отвечу только по сохранённым подтверждённым записям."
	}
	name := sess.PersonName
	if name == "" {
		name = "человеке"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Продолжим рассказ о %s. ", name)
	if sess.Mode == domain.ModeClarifyPerson {
		b.WriteString("Сейчас уточним детали. ")
	}
	if facts, err := d.store.ListFacts(ctx, domain.FactFilter{PersonID: deref(sess.PersonID), ConfirmedOnly: true, Limit: 5}); err == nil && len(facts) > 0 {
		b.WriteString("Уже известно: ")
		for i, f := range facts {
			if i > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "%s — %s", f.Type, f.Value)
		}
		b.WriteString(". ")
	}
	if qs, err := d.store.OpenQuestions(ctx, deref(sess.PersonID), 3); err == nil && len(qs) > 0 {
		b.WriteString("Открытые вопросы: ")
		for i, q := range qs {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(q.Text)
		}
		b.WriteString(".")
	} else {
		b.WriteString("Расскажите, что помните.")
	}
	return b.String()
}

// Turn — один шаг «пользователь сказал → помощник ответил».
func (d *Dialogue) Turn(ctx context.Context, sessionID string, audio []byte, filename string) (*TurnResult, error) {
	sess, err := d.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.Status != domain.SessionActive {
		return nil, ErrSessionNotActive
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("аудио пустое")
	}

	seqUser, err := d.store.NextTurnSeq(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// 1. Сохраняем аудио пользователя.
	userMedia, err := d.media.Upload(ctx, domain.MediaAudio, "session", sessionID, filename, audio, audioMIME(filename))
	if err != nil {
		return nil, fmt.Errorf("сохранить аудио: %w", err)
	}

	// 2. ASR. Транскодируем в WAV для совместимости с HF/whisper, если формат проблемный.
	asrAudio, asrName := transcodeForASR(ctx, audio, filename, d.logger)
	asrRes, err := d.ai.ASR.Transcribe(ctx, asrAudio, asrName, "ru")
	if err != nil {
		return nil, fmt.Errorf("asr: %w", err)
	}

	userTurn := &domain.Turn{
		SessionID:     sessionID,
		Seq:           seqUser,
		Role:          domain.RoleUser,
		Text:          asrRes.Text,
		AudioMediaID:  &userMedia.ID,
		Engine:        asrRes.Engine,
		Fallback:      asrRes.Fallback,
	}
	if err := d.store.AddTurn(ctx, userTurn); err != nil {
		return nil, err
	}
	userTurn.AudioURL = userMedia.URL

	// 3. Контекст и ответ интервьюера.
	recent, err := d.store.LastTurns(ctx, sessionID, 10)
	if err != nil {
		return nil, err
	}
	var facts []domain.Fact
	var questions []domain.Question
	if sess.PersonID != nil {
		facts, _ = d.store.ListFacts(ctx, domain.FactFilter{PersonID: *sess.PersonID, ConfirmedOnly: true, Limit: 20})
		questions, _ = d.store.OpenQuestions(ctx, *sess.PersonID, 10)
	}
	msgs := buildInterviewerMessages(sess.Mode, sess.PersonName, sess.ContextSummary, facts, questions, recent)
	chatRes, err := d.ai.LLM.Chat(ctx, ai.ChatRequest{
		Messages:    msgs,
		Purpose:     ai.PurposeInterview,
		Temperature: 0.4,
		MaxTokens:   220,
	})
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}

	// 4. TTS ответа.
	assistantTurn := &domain.Turn{
		SessionID: sessionID,
		Seq:       seqUser + 1,
		Role:      domain.RoleAssistant,
		Text:      chatRes.Text,
		Engine:    chatRes.Model,
		Fallback:  chatRes.Fallback,
	}
	if ttsRes, err := d.ai.TTS.Synthesize(ctx, chatRes.Text); err == nil {
		m, err := d.media.Upload(ctx, domain.MediaAudio, "session", sessionID, "reply."+ttsRes.Format, ttsRes.Audio, "audio/"+ttsRes.Format)
		if err == nil {
			assistantTurn.AudioMediaID = &m.ID
			assistantTurn.AudioURL = m.URL
			if ttsRes.Fallback {
				assistantTurn.Fallback = true
			}
		}
	} else {
		d.logger.Warn("TTS ответа недоступен", "session_id", sessionID, "err", err)
	}
	if err := d.store.AddTurn(ctx, assistantTurn); err != nil {
		return nil, err
	}

	// 5. Обновляем краткую сводку сессии.
	d.updateSummary(ctx, sessionID, sess.ContextSummary, recent)

	d.logger.Info("turn обработан", "session_id", sessionID, "seq", seqUser,
		"asr_engine", asrRes.Engine, "llm_engine", chatRes.Model)
	return &TurnResult{Session: sess, UserTurn: *userTurn, AssistantTurn: *assistantTurn}, nil
}

func (d *Dialogue) updateSummary(ctx context.Context, sessionID, prev string, recent []domain.Turn) {
	turns := append([]domain.Turn{}, recent...)
	chatRes, err := d.ai.LLM.Chat(ctx, ai.ChatRequest{
		Messages: buildSummaryMessages(prev, turns),
		Purpose:  ai.PurposeSummarize,
		MaxTokens: 160,
	})
	if err != nil || chatRes.Text == "" {
		// Heuristic fallback: последняя реплика пользователя.
		for i := len(turns) - 1; i >= 0; i-- {
			if turns[i].Role == domain.RoleUser && turns[i].Text != "" {
				_ = d.store.UpdateSessionSummary(ctx, sessionID, turns[i].Text)
				return
			}
		}
		return
	}
	_ = d.store.UpdateSessionSummary(ctx, sessionID, chatRes.Text)
}

func audioMIME(filename string) string {
	name := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(name, ".webm"):
		return "audio/webm"
	case strings.HasSuffix(name, ".ogg"), strings.HasSuffix(name, ".opus"):
		return "audio/ogg"
	case strings.HasSuffix(name, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(name, ".m4a"):
		return "audio/m4a"
	case strings.HasSuffix(name, ".aac"):
		return "audio/aac"
	case strings.HasSuffix(name, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(name, ".flac"):
		return "audio/flac"
	case strings.HasSuffix(name, ".amr"):
		return "audio/AMR"
	default:
		return "application/octet-stream"
	}
}

// transcodeForASR converts mobile/problematic audio formats to 16kHz mono WAV for ASR.
// On failure it returns the original bytes/filename so the provider can try as-is.
func transcodeForASR(ctx context.Context, audio []byte, filename string, logger *slog.Logger) ([]byte, string) {
	name := strings.ToLower(filename)
	// Formats usually accepted directly by whisper/HF.
	if strings.HasSuffix(name, ".wav") || strings.HasSuffix(name, ".flac") || strings.HasSuffix(name, ".mp3") || strings.HasSuffix(name, ".ogg") {
		return audio, filename
	}
	// Only attempt transcode when ffmpeg is available.
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return audio, filename
	}
	inExt := filepath.Ext(name)
	if inExt == "" {
		inExt = ".bin"
	}
	tmpDir, err := os.MkdirTemp("", "mb-asr-*")
	if err != nil {
		return audio, filename
	}
	defer os.RemoveAll(tmpDir)
	inPath := filepath.Join(tmpDir, "in"+inExt)
	outPath := filepath.Join(tmpDir, "out.wav")
	if err := os.WriteFile(inPath, audio, 0o600); err != nil {
		return audio, filename
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-i", inPath, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", outPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if logger != nil {
			logger.Warn("ffmpeg transcode for ASR failed, using original", "err", err, "stderr", strings.TrimSpace(stderr.String()), "filename", filename)
		}
		return audio, filename
	}
	out, err := os.ReadFile(outPath)
	if err != nil || len(out) == 0 {
		return audio, filename
	}
	outName := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".wav"
	if outName == ".wav" {
		outName = "audio.wav"
	}
	return out, outName
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Complete завершает сессию. Постобработка запускается синхронно или воркером.
func (d *Dialogue) Complete(ctx context.Context, sessionID string, sync bool, post *Postprocessor) error {
	sess, err := d.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.Status == domain.SessionCompleted {
		return nil
	}
	if err := d.store.CompleteSession(ctx, sessionID); err != nil {
		return err
	}
	if sync {
		if _, err := post.ProcessSession(ctx, sessionID); err != nil {
			return fmt.Errorf("постобработка: %w", err)
		}
		return nil
	}
	_, err = d.store.EnqueueJob(ctx, JobPostprocess, map[string]any{"session_id": sessionID})
	return err
}

// JobPostprocess — тип фоновой задачи постобработки.
const JobPostprocess = "postprocess"

var _ = time.Now
