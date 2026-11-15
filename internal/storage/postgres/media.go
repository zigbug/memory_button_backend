package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

const mediaCols = `id::text, kind, storage_key, url, mime, size_bytes, checksum, owner_kind, owner_id::text, created_at`

func scanMedia(row pgx.Row) (domain.Media, error) {
	var m domain.Media
	err := row.Scan(&m.ID, &m.Kind, &m.StorageKey, &m.URL, &m.MIME, &m.SizeBytes, &m.Checksum,
		&m.OwnerKind, &m.OwnerID, &m.CreatedAt)
	return m, err
}

func (s *Store) CreateMedia(ctx context.Context, m *domain.Media) error {
	const q = `INSERT INTO media (kind, storage_key, url, mime, size_bytes, checksum, owner_kind, owner_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::uuid)
		RETURNING id::text, created_at`
	err := s.pool.QueryRow(ctx, q, m.Kind, m.StorageKey, m.URL, m.MIME, m.SizeBytes, m.Checksum,
		m.OwnerKind, uuidPtr(m.OwnerID)).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return fmt.Errorf("create media: %w", err)
	}
	return nil
}

func (s *Store) GetMedia(ctx context.Context, id string) (domain.Media, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+mediaCols+` FROM media WHERE id = $1::uuid`, id)
	m, err := scanMedia(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMediaByOwner(ctx context.Context, ownerKind, ownerID string) ([]domain.Media, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+mediaCols+` FROM media
		WHERE owner_kind = $1 AND owner_id = $2::uuid ORDER BY created_at DESC`, ownerKind, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()
	out := []domain.Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
