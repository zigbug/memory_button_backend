package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationsTable = "schema_migrations"

// RunMigrations применяет все ещё не применённые up-миграции в транзакциях.
// Down-миграции не запускаются автоматически (только вручную при необходимости).
func RunMigrations(ctx context.Context, db *pgxpool.Pool, fsys fs.FS) error {
	if _, err := db.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`, migrationsTable)); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	applied := map[int64]bool{}
	rows, err := db.Query(ctx, fmt.Sprintf("SELECT version FROM %s", migrationsTable))
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, name := range files {
		version, migName, err := parseMigrationName(name)
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}
		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			"INSERT INTO %s (version, name, applied_at) VALUES ($1, $2, $3)",
			migrationsTable), version, migName, time.Now().UTC()); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func parseMigrationName(name string) (int64, string, error) {
	// 000001_init.up.sql -> 1, "init"
	base := strings.TrimSuffix(name, ".up.sql")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("некорректное имя миграции %q", name)
	}
	var version int64
	if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
		return 0, "", fmt.Errorf("некорректная версия в %q: %w", name, err)
	}
	return version, parts[1], nil
}
