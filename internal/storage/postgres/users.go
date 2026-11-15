package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, u *domain.User, passwordHash string) error {
	const q = `INSERT INTO users (username, password_hash) VALUES ($1, $2)
		RETURNING id::text, created_at`
	if err := s.pool.QueryRow(ctx, q, u.Username, passwordHash).Scan(&u.ID, &u.CreatedAt); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (domain.User, string, error) {
	var u domain.User
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT id::text, username, password_hash, created_at
		FROM users WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &hash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, "", ErrNotFound
	}
	if err != nil {
		return u, "", err
	}
	return u, hash, nil
}

// EnsureUser создаёт single-user аккаунт при старте, если его ещё нет.
func (s *Store) EnsureUser(ctx context.Context, username, passwordHash string) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)`, username).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	u := domain.User{Username: username}
	return s.CreateUser(ctx, &u, passwordHash)
}
