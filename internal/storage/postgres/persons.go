package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"memorybutton/backend/internal/domain"
)

const personCols = `id::text, display_name, name_variants, birth_date, birth_approx, death_date,
	death_approx, gender, places, notes, photo_media_id::text, created_at, updated_at`

func scanPerson(row pgx.Row) (domain.Person, error) {
	var p domain.Person
	var birthDate, deathDate, gender *string
	err := row.Scan(&p.ID, &p.DisplayName, &p.NameVariants, &birthDate, &p.BirthApprox,
		&deathDate, &p.DeathApprox, &gender, &p.Places, &p.Notes, &p.PhotoMediaID,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.BirthDate = derefString(birthDate)
	p.DeathDate = derefString(deathDate)
	p.Gender = derefString(gender)
	if p.NameVariants == nil {
		p.NameVariants = []string{}
	}
	if p.Places == nil {
		p.Places = []string{}
	}
	return p, nil
}

func (s *Store) CreatePerson(ctx context.Context, p *domain.Person) error {
	const q = `INSERT INTO persons
		(display_name, name_variants, birth_date, birth_approx, death_date, death_approx, gender, places, notes, photo_media_id)
		VALUES ($1, $2::text[], $3, $4, $5, $6, $7, $8::text[], $9, $10::uuid)
		RETURNING id::text, created_at, updated_at`
	err := s.pool.QueryRow(ctx, q, p.DisplayName, p.NameVariants, strPtr(p.BirthDate), p.BirthApprox,
		strPtr(p.DeathDate), p.DeathApprox, strPtr(p.Gender), p.Places, p.Notes, uuidPtr(p.PhotoMediaID)).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create person: %w", err)
	}
	return nil
}

func (s *Store) GetPerson(ctx context.Context, id string) (domain.Person, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+personCols+` FROM persons WHERE id = $1::uuid`, id)
	p, err := scanPerson(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

type PersonFilter struct {
	Search string
	Limit  int
	Offset int
}

func (s *Store) ListPersons(ctx context.Context, f PersonFilter) ([]domain.Person, int, error) {
	where := ""
	args := []any{}
	if f.Search != "" {
		where = `WHERE display_name ILIKE $1 OR EXISTS (SELECT 1 FROM unnest(name_variants) v WHERE v ILIKE $1)`
		args = append(args, "%"+f.Search+"%")
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM persons `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count persons: %w", err)
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	q := `SELECT ` + personCols + ` FROM persons ` + where +
		fmt.Sprintf(` ORDER BY display_name LIMIT %d OFFSET %d`, limit, offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list persons: %w", err)
	}
	defer rows.Close()
	out := []domain.Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdatePerson(ctx context.Context, p *domain.Person) error {
	const q = `UPDATE persons SET
		display_name = $2, name_variants = $3::text[], birth_date = $4, birth_approx = $5,
		death_date = $6, death_approx = $7, gender = $8, places = $9::text[], notes = $10,
		photo_media_id = $11::uuid
		WHERE id = $1::uuid RETURNING created_at, updated_at`
	err := s.pool.QueryRow(ctx, q, p.ID, p.DisplayName, p.NameVariants, strPtr(p.BirthDate), p.BirthApprox,
		strPtr(p.DeathDate), p.DeathApprox, strPtr(p.Gender), p.Places, p.Notes, uuidPtr(p.PhotoMediaID)).
		Scan(&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("update person: %w", err)
	}
	return nil
}

func (s *Store) SetPersonPhoto(ctx context.Context, personID, mediaID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE persons SET photo_media_id = $2::uuid WHERE id = $1::uuid`, personID, mediaID)
	if err != nil {
		return fmt.Errorf("set person photo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PersonName возвращает отображаемое имя человека (нужно для подсказки в сессии).
func (s *Store) PersonName(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT display_name FROM persons WHERE id = $1::uuid`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

// SearchPersonsByNames используется экстрактором: ищет людей по точному или
// нечёткому совпадению ФИО/вариантов, чтобы не плодить дубликаты.
func (s *Store) SearchPersonsByNames(ctx context.Context, names []string) (map[string]domain.Person, error) {
	out := map[string]domain.Person{}
	if len(names) == 0 {
		return out, nil
	}
	norm := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" {
			norm = append(norm, n)
		}
	}
	if len(norm) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (id) `+personCols+`
		FROM persons
		WHERE lower(display_name) = ANY($1::text[])
		   OR EXISTS (SELECT 1 FROM unnest(name_variants) v WHERE lower(v) = ANY($1::text[]))
		ORDER BY id`, norm)
	if err != nil {
		return nil, fmt.Errorf("search persons: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out[strings.ToLower(p.DisplayName)] = p
		for _, v := range p.NameVariants {
			out[strings.ToLower(v)] = p
		}
	}
	return out, rows.Err()
}

// ---------- Карточка человека ----------

func (s *Store) PersonCard(ctx context.Context, id string) (domain.PersonCard, error) {
	p, err := s.GetPerson(ctx, id)
	if err != nil {
		return domain.PersonCard{}, err
	}
	card := domain.PersonCard{Person: p}

	rels, err := s.listRelationships(ctx, id)
	if err != nil {
		return card, err
	}
	card.Relationships = rels

	stories, err := s.listStoriesByPerson(ctx, id)
	if err != nil {
		return card, err
	}
	card.Stories = stories

	facts, err := s.ListFacts(ctx, domain.FactFilter{PersonID: id, ConfirmedOnly: true})
	if err != nil {
		return card, err
	}
	card.Facts = facts

	questions, _, err := s.ListQuestions(ctx, domain.QuestionFilter{PersonID: id})
	if err != nil {
		return card, err
	}
	card.Questions = questions
	return card, nil
}
