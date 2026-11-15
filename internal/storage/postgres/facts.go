package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

const factCols = `id::text, person_id::text, story_id::text, session_id::text, type, value,
	normalized, confidence, source_ref, source_excerpt, confirmed, created_at, updated_at`

func scanFact(row pgx.Row) (domain.Fact, error) {
	var f domain.Fact
	var personID, storyID, sessionID *string
	var normalized []byte
	var confidence *float64
	err := row.Scan(&f.ID, &personID, &storyID, &sessionID, &f.Type, &f.Value, &normalized,
		&confidence, &f.SourceRef, &f.SourceExcerpt, &f.Confirmed, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return f, err
	}
	f.PersonID = personID
	f.StoryID = storyID
	f.SessionID = sessionID
	if confidence != nil {
		f.Confidence = *confidence
	}
	if err := scanJSON(normalized, &f.Normalized); err != nil {
		return f, err
	}
	return f, nil
}

func (s *Store) CreateFact(ctx context.Context, f *domain.Fact) error {
	norm, err := jsonArg(f.Normalized)
	if err != nil {
		return err
	}
	const q = `INSERT INTO facts
		(person_id, story_id, session_id, type, value, normalized, confidence, source_ref, source_excerpt, confirmed)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::jsonb, $7, $8, $9, $10)
		RETURNING id::text, created_at, updated_at`
	err = s.pool.QueryRow(ctx, q, uuidPtr(f.PersonID), uuidPtr(f.StoryID), uuidPtr(f.SessionID),
		f.Type, f.Value, norm, f.Confidence, f.SourceRef, f.SourceExcerpt, f.Confirmed).
		Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create fact: %w", err)
	}
	return nil
}

func (s *Store) GetFact(ctx context.Context, id string) (domain.Fact, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+factCols+` FROM facts WHERE id = $1::uuid`, id)
	f, err := scanFact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (s *Store) ListFacts(ctx context.Context, f domain.FactFilter) ([]domain.Fact, error) {
	where := "WHERE ($1 = '' OR person_id = $1::uuid) AND ($2 = '' OR story_id = $2::uuid) AND ($3 = '' OR type = $3)"
	args := []any{f.PersonID, f.StoryID, f.Type}
	if f.ConfirmedOnly {
		where += " AND confirmed"
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `SELECT ` + factCols + ` FROM facts ` + where +
		fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d OFFSET %d`, limit, offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}
	defer rows.Close()
	out := []domain.Fact{}
	for rows.Next() {
		fact, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fact)
	}
	return out, rows.Err()
}

// SearchConfirmedFacts — гружёжный поиск для Q&A: ищет по person_id или по подстроке в value.
func (s *Store) SearchConfirmedFacts(ctx context.Context, personID, query string, limit int) ([]domain.Fact, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT ` + factCols + ` FROM facts WHERE confirmed`
	args := []any{}
	if personID != "" {
		args = append(args, personID)
		q += fmt.Sprintf(` AND person_id = $%d::uuid`, len(args))
	}
	if query != "" {
		args = append(args, "%"+strings.ToLower(query)+"%")
		q += fmt.Sprintf(` AND lower(value) LIKE $%d`, len(args))
	}
	q += fmt.Sprintf(` ORDER BY confidence DESC, created_at DESC LIMIT %d`, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search facts: %w", err)
	}
	defer rows.Close()
	out := []domain.Fact{}
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
