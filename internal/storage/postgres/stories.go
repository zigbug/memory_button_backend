package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

const storyCols = `id::text, session_id::text, text, status, version, created_at, updated_at`

func scanStory(row pgx.Row) (domain.Story, error) {
	var st domain.Story
	err := row.Scan(&st.ID, &st.SessionID, &st.Text, &st.Status, &st.Version, &st.CreatedAt, &st.UpdatedAt)
	return st, err
}

func (s *Store) CreateStory(ctx context.Context, st *domain.Story) error {
	return s.TxRunner(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO stories (session_id, text, status, version)
			VALUES ($1::uuid, $2, $3, $4) RETURNING id::text, created_at, updated_at`,
			uuidPtr(st.SessionID), st.Text, st.Status, st.Version).
			Scan(&st.ID, &st.CreatedAt, &st.UpdatedAt)
		if err != nil {
			return fmt.Errorf("create story: %w", err)
		}
		for _, pid := range st.PersonIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO story_persons (story_id, person_id) VALUES ($1::uuid, $2::uuid)
				 ON CONFLICT DO NOTHING`, st.ID, pid); err != nil {
				return fmt.Errorf("link story person: %w", err)
			}
		}
		return nil
	})
}

func (s *Store) GetStory(ctx context.Context, id string) (domain.Story, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+storyCols+` FROM stories WHERE id = $1::uuid`, id)
	st, err := scanStory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, ErrNotFound
	}
	if err != nil {
		return st, err
	}
	st.PersonIDs, err = s.storyPersonIDs(ctx, id)
	return st, err
}

func (s *Store) storyPersonIDs(ctx context.Context, id string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT person_id::text FROM story_persons WHERE story_id = $1::uuid ORDER BY person_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) listStoriesByPerson(ctx context.Context, personID string) ([]domain.Story, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+storyCols+` FROM stories st
		JOIN story_persons sp ON sp.story_id = st.id
		WHERE sp.person_id = $1::uuid ORDER BY st.created_at DESC`, personID)
	if err != nil {
		return nil, fmt.Errorf("list stories by person: %w", err)
	}
	defer rows.Close()
	out := []domain.Story{}
	for rows.Next() {
		st, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) ListStoriesByPerson(ctx context.Context, personID string) ([]domain.Story, error) {
	return s.listStoriesByPerson(ctx, personID)
}

func (s *Store) ListStoriesBySession(ctx context.Context, sessionID string) ([]domain.Story, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+storyCols+` FROM stories
		WHERE session_id = $1::uuid ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list stories by session: %w", err)
	}
	defer rows.Close()
	out := []domain.Story{}
	for rows.Next() {
		st, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ListConfirmedStories — все подтверждённые рассказы (для Q&A по всем людям).
func (s *Store) ListConfirmedStories(ctx context.Context, limit int) ([]domain.Story, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT `+storyCols+` FROM stories
		WHERE status = 'confirmed' ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list confirmed stories: %w", err)
	}
	defer rows.Close()
	out := []domain.Story{}
	for rows.Next() {
		st, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ids, err := s.storyPersonIDs(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].PersonIDs = ids
	}
	return out, nil
}

func (s *Store) UpdateStoryText(ctx context.Context, id, text string, bumpVersion bool) error {
	var versionExpr string
	if bumpVersion {
		versionExpr = "version = version + 1"
	} else {
		versionExpr = "version = version"
	}
	tag, err := s.pool.Exec(ctx, `UPDATE stories SET text = $2, `+versionExpr+` WHERE id = $1::uuid`, id, text)
	if err != nil {
		return fmt.Errorf("update story: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetStoryStatus(ctx context.Context, id string, status domain.StoryStatus) error {
	tag, err := s.pool.Exec(ctx, `UPDATE stories SET status = $2 WHERE id = $1::uuid`, id, status)
	if err != nil {
		return fmt.Errorf("set story status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ReplaceStoryPersons(ctx context.Context, storyID string, personIDs []string) error {
	return s.TxRunner(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM story_persons WHERE story_id = $1::uuid`, storyID); err != nil {
			return err
		}
		for _, pid := range personIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO story_persons (story_id, person_id) VALUES ($1::uuid, $2::uuid)
				 ON CONFLICT DO NOTHING`, storyID, pid); err != nil {
				return err
			}
		}
		return nil
	})
}
