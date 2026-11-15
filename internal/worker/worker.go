// Package worker — фоновый обработчик очереди (постобработка сессий).
//
// В MVP используется таблица jobs как outbox в PostgreSQL (FOR UPDATE SKIP LOCKED),
// что достаточно для одного пользователя и не требует брокера сообщений.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"memorybutton/backend/internal/pipeline"
	"memorybutton/backend/internal/storage/postgres"
)

const maxAttempts = 3

type Worker struct {
	store    *postgres.Store
	post     *pipeline.Postprocessor
	logger   *slog.Logger
	interval time.Duration
}

func New(store *postgres.Store, post *pipeline.Postprocessor, logger *slog.Logger, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Worker{store: store, post: post, logger: logger, interval: interval}
}

// Run крутится до отмены контекста.
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("воркер запущен", "poll_interval", w.interval.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("воркер остановлен")
			return nil
		case <-ticker.C:
			if err := w.step(ctx); err != nil && !errors.Is(err, postgres.ErrNotFound) {
				w.logger.Error("ошибка шага воркера", "err", err)
			}
		}
	}
}

func (w *Worker) step(ctx context.Context) error {
	job, err := w.store.ClaimNextJob(ctx, []string{pipeline.JobPostprocess})
	if err != nil {
		return err
	}
	sessionID, _ := job.Payload["session_id"].(string)
	if sessionID == "" {
		_ = w.store.FinishJob(ctx, job.ID, "в задаче нет session_id")
		return nil
	}
	w.logger.Info("постобработка сессии", "job_id", job.ID, "session_id", sessionID, "attempt", job.Attempts)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := w.post.ProcessSession(ctx, sessionID); err != nil {
		remaining, rerr := w.store.RequeueJob(ctx, job.ID, err.Error(), maxAttempts)
		if rerr != nil {
			w.logger.Error("не удалось обновить задачу", "job_id", job.ID, "err", rerr)
		}
		w.logger.Warn("постобработка не удалась", "job_id", job.ID, "session_id", sessionID,
			"attempts_left", maxAttempts-remaining+1, "err", err)
		return nil
	}
	if err := w.store.FinishJob(ctx, job.ID, ""); err != nil {
		return err
	}
	w.logger.Info("постобработка завершена", "job_id", job.ID, "session_id", sessionID)
	return nil
}
