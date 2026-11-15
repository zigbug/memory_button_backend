package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

func (s *Store) EnqueueJob(ctx context.Context, jobType string, payload map[string]any) (domain.Job, error) {
	var job domain.Job
	body, err := json.Marshal(payload)
	if err != nil {
		return job, err
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO jobs (type, payload) VALUES ($1, $2::jsonb)
		RETURNING id::text, status, attempts, created_at, updated_at`, jobType, string(body)).
		Scan(&job.ID, &job.Status, &job.Attempts, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return job, fmt.Errorf("enqueue job: %w", err)
	}
	job.Type = jobType
	return job, nil
}

// ClaimNextJob атомарно забирает очередную задачу (FOR UPDATE SKIP LOCKED).
func (s *Store) ClaimNextJob(ctx context.Context, types []string) (domain.Job, error) {
	var job domain.Job
	var payload []byte
	q := `SELECT id::text, type, payload, status, attempts, created_at, updated_at
		FROM jobs WHERE status = 'pending'`
	args := []any{}
	if len(types) > 0 {
		args = append(args, types)
		q += fmt.Sprintf(` AND type = ANY($%d)`, len(args))
	}
	q += ` ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return job, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = tx.QueryRow(ctx, q, args...).Scan(&job.ID, &job.Type, &payload, &job.Status,
		&job.Attempts, &job.CreatedAt, &job.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return job, ErrNotFound
		}
		return job, err
	}
	if _, err = tx.Exec(ctx, `UPDATE jobs SET status = 'processing', attempts = attempts + 1,
		updated_at = now() WHERE id = $1::uuid`, job.ID); err != nil {
		return job, err
	}
	if err = tx.Commit(ctx); err != nil {
		return job, err
	}
	_ = json.Unmarshal(payload, &job.Payload)
	job.Status = domain.JobProcessing
	job.Attempts++
	return job, nil
}

// JobPayload хранит параметры задачи (например, session_id).
type JobPayload struct {
	SessionID string `json:"session_id"`
	DraftID   string `json:"draft_id"`
}

func (s *Store) FinishJob(ctx context.Context, id string, lastErr string) error {
	status := domain.JobDone
	if lastErr != "" {
		status = domain.JobFailed
	}
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET status = $2, last_error = $3, updated_at = now()
		WHERE id = $1::uuid`, id, status, lastErr)
	return err
}

// RequeueJob переводит задачу в pending, если попыток было меньше maxAttempts,
// иначе помечает failed. Возвращает число оставшихся попыток.
func (s *Store) RequeueJob(ctx context.Context, id, errMsg string, maxAttempts int) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `UPDATE jobs SET
		status = CASE WHEN attempts < $3 THEN 'pending' ELSE 'failed' END,
		last_error = $2, updated_at = now()
		WHERE id = $1::uuid RETURNING attempts`, id, errMsg, maxAttempts).Scan(&attempts)
	return attempts, err
}

// RetryJobs возвращает failed-задачи в pending (ручной перезапуск/для простоты MVP).
func (s *Store) RetryJob(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET status = 'pending', last_error = '', updated_at = now()
		WHERE id = $1::uuid AND status = 'failed'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListJobs(ctx context.Context, limit int) ([]domain.Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, type, status, attempts, last_error, created_at, updated_at
		FROM jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Job{}
	for rows.Next() {
		var j domain.Job
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
