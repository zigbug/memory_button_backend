package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

const sessionCols = `s.id::text, s.mode, s.person_id::text, p.display_name, s.status,
	s.context_summary, s.meta, s.started_at, s.ended_at, s.created_at, s.updated_at`

func scanSession(row pgx.Row) (domain.Session, error) {
	var s domain.Session
	var personID, personName *string
	var meta []byte
	err := row.Scan(&s.ID, &s.Mode, &personID, &personName, &s.Status, &s.ContextSummary,
		&meta, &s.StartedAt, &s.EndedAt, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	s.PersonID = personID
	s.PersonName = derefString(personName)
	_ = scanJSON(meta, &s.QuestionIDs)
	if s.QuestionIDs == nil {
		s.QuestionIDs = []string{}
	}
	return s, nil
}

func (s *Store) CreateSession(ctx context.Context, sess *domain.Session) error {
	meta, err := jsonArg(map[string]any{"question_ids": sess.QuestionIDs})
	if err != nil {
		return err
	}
	const q = `INSERT INTO sessions (mode, person_id, status, context_summary, meta)
		VALUES ($1, $2::uuid, $3, $4, $5::jsonb)
		RETURNING id::text, started_at, created_at, updated_at`
	err = s.pool.QueryRow(ctx, q, sess.Mode, uuidPtr(sess.PersonID), sess.Status, sess.ContextSummary, meta).
		Scan(&sess.ID, &sess.StartedAt, &sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (domain.Session, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM sessions s
		LEFT JOIN persons p ON p.id = s.person_id WHERE s.id = $1::uuid`, id)
	sess, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, ErrNotFound
	}
	return sess, err
}

func (s *Store) ListSessions(ctx context.Context, status, mode, personID string, limit, offset int) ([]domain.Session, error) {
	where := "WHERE ($1 = '' OR s.status = $1) AND ($2 = '' OR s.mode = $2) AND ($3 = '' OR s.person_id = $3::uuid)"
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT ` + sessionCols + ` FROM sessions s LEFT JOIN persons p ON p.id = s.person_id ` + where +
		fmt.Sprintf(` ORDER BY s.created_at DESC LIMIT %d OFFSET %d`, limit, offset)
	rows, err := s.pool.Query(ctx, q, status, mode, personID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSessionSummary(ctx context.Context, id, summary string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET context_summary = $2 WHERE id = $1::uuid`, id, summary)
	if err != nil {
		return fmt.Errorf("update session summary: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CompleteSession(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = 'completed', ended_at = now() WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("complete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ArchiveSession(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET status = 'archived' WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("archive session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Реплики ----------

func (s *Store) NextTurnSeq(ctx context.Context, sessionID string) (int, error) {
	var seq int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(max(seq), 0) + 1 FROM turns WHERE session_id = $1::uuid`, sessionID).Scan(&seq)
	return seq, err
}

func (s *Store) AddTurn(ctx context.Context, t *domain.Turn) error {
	const q = `INSERT INTO turns (session_id, seq, role, text, audio_media_id, meta)
		VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6::jsonb)
		RETURNING id::text, created_at`
	meta, err := jsonArg(turnMeta(t))
	if err != nil {
		return err
	}
	err = s.pool.QueryRow(ctx, q, t.SessionID, t.Seq, t.Role, t.Text, uuidPtr(t.AudioMediaID), meta).
		Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("add turn: %w", err)
	}
	return nil
}

func (s *Store) ListTurns(ctx context.Context, sessionID string) ([]domain.Turn, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, session_id::text, seq, role, text, audio_media_id::text,
		       meta, created_at
		FROM turns WHERE session_id = $1::uuid ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	defer rows.Close()
	out := []domain.Turn{}
	for rows.Next() {
		var t domain.Turn
		var meta []byte
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Seq, &t.Role, &t.Text, &t.AudioMediaID, &meta, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Engine, t.Fallback = scanTurnMeta(meta)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) LastTurns(ctx context.Context, sessionID string, n int) ([]domain.Turn, error) {
	if n <= 0 || n > 100 {
		n = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, session_id::text, seq, role, text, audio_media_id::text, meta, created_at
		FROM turns WHERE session_id = $1::uuid ORDER BY seq DESC LIMIT $2`, sessionID, n)
	if err != nil {
		return nil, fmt.Errorf("last turns: %w", err)
	}
	defer rows.Close()
	out := []domain.Turn{}
	for rows.Next() {
		var t domain.Turn
		var meta []byte
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Seq, &t.Role, &t.Text, &t.AudioMediaID, &meta, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Engine, t.Fallback = scanTurnMeta(meta)
		out = append(out, t)
	}
	// Разворачиваем в хронологическом порядке.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// ---------- Транскрипты ----------

func (s *Store) SaveTranscript(ctx context.Context, tr *domain.Transcript) error {
	segments, err := jsonArg(tr.Segments)
	if err != nil {
		return err
	}
	const q = `INSERT INTO transcripts (session_id, segments, engine)
		VALUES ($1::uuid, $2::jsonb, $3) RETURNING id::text, created_at`
	return s.pool.QueryRow(ctx, q, tr.SessionID, segments, tr.Engine).Scan(&tr.ID, &tr.CreatedAt)
}

func (s *Store) LatestTranscript(ctx context.Context, sessionID string) (domain.Transcript, error) {
	var tr domain.Transcript
	var segments []byte
	err := s.pool.QueryRow(ctx, `SELECT id::text, session_id::text, segments, engine, created_at
		FROM transcripts WHERE session_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, sessionID).
		Scan(&tr.ID, &tr.SessionID, &segments, &tr.Engine, &tr.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return tr, ErrNotFound
	}
	if err != nil {
		return tr, err
	}
	if err := scanJSON(segments, &tr.Segments); err != nil {
		return tr, err
	}
	return tr, nil
}

func turnMeta(t *domain.Turn) map[string]any {
	m := map[string]any{}
	if t.Engine != "" {
		m["engine"] = t.Engine
	}
	if t.Fallback {
		m["fallback"] = true
	}
	return m
}

func scanTurnMeta(b []byte) (engine string, fallback bool) {
	if len(b) == 0 {
		return "", false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", false
	}
	if v, ok := m["engine"].(string); ok {
		engine = v
	}
	if v, ok := m["fallback"].(bool); ok {
		fallback = v
	}
	return engine, fallback
}

// SessionStartedAt помогает воркеру не обрабатывать слишком свежие сессии.
func (s *Store) SessionStartedAt(ctx context.Context, id string) (time.Time, error) {
	var t time.Time
	err := s.pool.QueryRow(ctx, `SELECT started_at FROM sessions WHERE id = $1::uuid`, id).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

var _ = json.Marshal
