package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"memorybutton/backend/internal/domain"
)

const questionCols = `id::text, person_id::text, text, source, reason, status, priority,
	related_fact_id::text, related_session_id::text, created_at, updated_at, resolved_at`

func scanQuestion(row pgx.Row) (domain.Question, error) {
	var q domain.Question
	var personID, relatedFactID, relatedSessionID *string
	err := row.Scan(&q.ID, &personID, &q.Text, &q.Source, &q.Reason, &q.Status, &q.Priority,
		&relatedFactID, &q.RelatedSessionID, &q.CreatedAt, &q.UpdatedAt, &q.ResolvedAt)
	if err != nil {
		return q, err
	}
	q.PersonID = personID
	q.RelatedFactID = relatedFactID
	q.RelatedSessionID = relatedSessionID
	return q, nil
}

// CreateQuestion вставляет вопрос; при дубликате (person_id, lower(text))
// возвращает существующую запись, чтобы реестр не засорялся повторами.
func (s *Store) CreateQuestion(ctx context.Context, q *domain.Question) error {
	const qIns = `INSERT INTO questions
		(person_id, text, source, reason, status, priority, related_fact_id, related_session_id)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, $8::uuid)
		RETURNING id::text, created_at, updated_at`
	err := s.pool.QueryRow(ctx, qIns, uuidPtr(q.PersonID), q.Text, q.Source, q.Reason, q.Status,
		q.Priority, uuidPtr(q.RelatedFactID), uuidPtr(q.RelatedSessionID)).
		Scan(&q.ID, &q.CreatedAt, &q.UpdatedAt)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		pid := ""
		if q.PersonID != nil {
			pid = *q.PersonID
		}
		return s.findQuestion(ctx, pid, q.Text, q)
	}
	return fmt.Errorf("create question: %w", err)
}

func (s *Store) findQuestion(ctx context.Context, personID, text string, dst *domain.Question) error {
	var pidArg any
	if personID != "" {
		pidArg = personID
	}
	row := s.pool.QueryRow(ctx, `SELECT `+questionCols+` FROM questions
		WHERE COALESCE(person_id, '00000000-0000-0000-0000-000000000000'::uuid)
		      = COALESCE($1::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
		  AND lower(text) = lower($2)`, pidArg, text)
	got, err := scanQuestion(row)
	if err != nil {
		return fmt.Errorf("find duplicate question: %w", err)
	}
	*dst = got
	return nil
}

func (s *Store) GetQuestion(ctx context.Context, id string) (domain.Question, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+questionCols+` FROM questions WHERE id = $1::uuid`, id)
	q, err := scanQuestion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, ErrNotFound
	}
	return q, err
}

func (s *Store) ListQuestions(ctx context.Context, f domain.QuestionFilter) ([]domain.Question, int, error) {
	where := "WHERE ($1 = '' OR person_id = $1::uuid) AND ($2 = '' OR status = $2) AND ($3 = '' OR source = $3)"
	args := []any{f.PersonID, f.Status, f.Source}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM questions `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count questions: %w", err)
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `SELECT ` + questionCols + ` FROM questions ` + where +
		fmt.Sprintf(` ORDER BY priority DESC, created_at DESC LIMIT %d OFFSET %d`, limit, offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()
	out := []domain.Question{}
	for rows.Next() {
		item, err := scanQuestion(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateQuestion(ctx context.Context, q *domain.Question) error {
	const qUpd = `UPDATE questions SET text = $2, status = $3, priority = $4, reason = $5,
		related_fact_id = $6::uuid, related_session_id = $7::uuid,
		resolved_at = CASE WHEN $3 = 'resolved' AND resolved_at IS NULL THEN now() ELSE resolved_at END
		WHERE id = $1::uuid RETURNING updated_at`
	err := s.pool.QueryRow(ctx, qUpd, q.ID, q.Text, q.Status, q.Priority, q.Reason,
		uuidPtr(q.RelatedFactID), uuidPtr(q.RelatedSessionID)).Scan(&q.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update question: %w", err)
	}
	return nil
}

func (s *Store) SetQuestionStatus(ctx context.Context, id string, status domain.QuestionStatus) error {
	tag, err := s.pool.Exec(ctx, `UPDATE questions SET status = $2,
		resolved_at = CASE WHEN $2 = 'resolved' AND resolved_at IS NULL THEN now() ELSE resolved_at END
		WHERE id = $1::uuid`, id, status)
	if err != nil {
		return fmt.Errorf("set question status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// OpenQuestions возвращает открытые вопросы по человеку (или общие), наиболее приоритетные первыми.
func (s *Store) OpenQuestions(ctx context.Context, personID string, limit int) ([]domain.Question, error) {
	limit2 := limit
	if limit2 <= 0 || limit2 > 100 {
		limit2 = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT `+questionCols+` FROM questions
		WHERE status = 'open' AND (person_id = $1::uuid OR person_id IS NULL)
		ORDER BY priority DESC, created_at ASC LIMIT $2`, uuidPtr(&personID), limit2)
	if err != nil {
		return nil, fmt.Errorf("open questions: %w", err)
	}
	defer rows.Close()
	out := []domain.Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
