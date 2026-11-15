package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

const relCols = `r.id::text, r.from_person_id::text, r.to_person_id::text,
	f.display_name, t.display_name, r.type, r.confidence, r.source, r.note, r.created_at, r.updated_at`

func scanRelationship(row pgx.Row) (domain.Relationship, error) {
	var r domain.Relationship
	err := row.Scan(&r.ID, &r.FromPersonID, &r.ToPersonID, &r.FromPersonName, &r.ToPersonName,
		&r.Type, &r.Confidence, &r.Source, &r.Note, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Store) CreateRelationship(ctx context.Context, r *domain.Relationship) error {
	const q = `INSERT INTO relationships (from_person_id, to_person_id, type, confidence, source, note)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
		RETURNING id::text, created_at, updated_at`
	err := s.pool.QueryRow(ctx, q, r.FromPersonID, r.ToPersonID, r.Type, r.Confidence, r.Source, r.Note).
		Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create relationship: %w", err)
	}
	return nil
}

func (s *Store) GetRelationship(ctx context.Context, id string) (domain.Relationship, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+relCols+`
		FROM relationships r
		JOIN persons f ON f.id = r.from_person_id
		JOIN persons t ON t.id = r.to_person_id
		WHERE r.id = $1::uuid`, id)
	r, err := scanRelationship(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// ListRelationships возвращает все связи; при personID — только связанные с человеком.
func (s *Store) ListRelationships(ctx context.Context, personID string) ([]domain.Relationship, error) {
	return s.listRelationshipsWhere(ctx, personID)
}

func (s *Store) listRelationships(ctx context.Context, personID string) ([]domain.Relationship, error) {
	return s.listRelationshipsWhere(ctx, personID)
}

func (s *Store) listRelationshipsWhere(ctx context.Context, personID string) ([]domain.Relationship, error) {
	q := `SELECT ` + relCols + ` FROM relationships r
		JOIN persons f ON f.id = r.from_person_id
		JOIN persons t ON t.id = r.to_person_id`
	args := []any{}
	if personID != "" {
		q += ` WHERE r.from_person_id = $1::uuid OR r.to_person_id = $1::uuid`
		args = append(args, personID)
	}
	q += ` ORDER BY r.created_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}
	defer rows.Close()
	out := []domain.Relationship{}
	for rows.Next() {
		r, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateRelationship(ctx context.Context, r *domain.Relationship) error {
	const q = `UPDATE relationships SET from_person_id = $2::uuid, to_person_id = $3::uuid,
		type = $4, confidence = $5, source = $6, note = $7
		WHERE id = $1::uuid RETURNING updated_at`
	err := s.pool.QueryRow(ctx, q, r.ID, r.FromPersonID, r.ToPersonID, r.Type, r.Confidence, r.Source, r.Note).
		Scan(&r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update relationship: %w", err)
	}
	return nil
}

func (s *Store) DeleteRelationship(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM relationships WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete relationship: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
