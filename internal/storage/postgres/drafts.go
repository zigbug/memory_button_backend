package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"memorybutton/backend/internal/domain"
)

const draftCols = `id::text, session_id::text, status, story_text, meta, created_at, updated_at, confirmed_at`
const draftItemCols = `id::text, draft_id::text, kind, action, payload, status, resolved_ref_id::text,
	created_at, updated_at`

func scanDraft(row pgx.Row) (domain.Draft, error) {
	var d domain.Draft
	var meta []byte
	err := row.Scan(&d.ID, &d.SessionID, &d.Status, &d.StoryText, &meta,
		&d.CreatedAt, &d.UpdatedAt, &d.ConfirmedAt)
	if err != nil {
		return d, err
	}
	_ = scanJSON(meta, &d.Meta)
	return d, nil
}

func scanDraftItem(row pgx.Row) (domain.DraftItem, error) {
	var it domain.DraftItem
	var payload []byte
	err := row.Scan(&it.ID, &it.DraftID, &it.Kind, &it.Action, &payload, &it.Status,
		&it.ResolvedRefID, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return it, err
	}
	if err := scanJSON(payload, &it.Payload); err != nil {
		return it, err
	}
	return it, nil
}

func (s *Store) CreateDraft(ctx context.Context, d *domain.Draft) error {
	meta, err := jsonArg(d.Meta)
	if err != nil {
		return err
	}
	const q = `INSERT INTO drafts (session_id, status, story_text, meta)
		VALUES ($1::uuid, $2, $3, $4::jsonb) RETURNING id::text, created_at, updated_at`
	return s.pool.QueryRow(ctx, q, d.SessionID, d.Status, d.StoryText, meta).
		Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

func (s *Store) GetDraft(ctx context.Context, id string) (domain.Draft, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+draftCols+` FROM drafts WHERE id = $1::uuid`, id)
	d, err := scanDraft(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	items, err := s.draftItems(ctx, txOrPool(s.pool), id)
	if err != nil {
		return d, err
	}
	d.Items = items
	return d, nil
}

// GetDraftBySession возвращает последний черновик сессии.
func (s *Store) GetDraftBySession(ctx context.Context, sessionID string) (domain.Draft, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+draftCols+` FROM drafts
		WHERE session_id = $1::uuid ORDER BY created_at DESC LIMIT 1`, sessionID)
	d, err := scanDraft(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	items, err := s.draftItems(ctx, txOrPool(s.pool), d.ID)
	if err != nil {
		return d, err
	}
	d.Items = items
	return d, nil
}

func (s *Store) draftItems(ctx context.Context, q querier, draftID string) ([]domain.DraftItem, error) {
	rows, err := q.Query(ctx, `SELECT `+draftItemCols+` FROM draft_items
		WHERE draft_id = $1::uuid ORDER BY created_at, id`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DraftItem{}
	for rows.Next() {
		it, err := scanDraftItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// querier позволяет переиспользовать запросы и в пуле, и в транзакции.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func txOrPool(p *pgxpool.Pool) querier { return p }

func (s *Store) AddDraftItem(ctx context.Context, it *domain.DraftItem) error {
	payload, err := jsonArg(it.Payload)
	if err != nil {
		return err
	}
	const q = `INSERT INTO draft_items (draft_id, kind, action, payload, status)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5) RETURNING id::text, created_at, updated_at`
	return s.pool.QueryRow(ctx, q, it.DraftID, it.Kind, it.Action, payload, it.Status).
		Scan(&it.ID, &it.CreatedAt, &it.UpdatedAt)
}

func (s *Store) SetDraftStatus(ctx context.Context, id string, status domain.DraftStatus) error {
	tag, err := s.pool.Exec(ctx, `UPDATE drafts SET status = $2,
		confirmed_at = CASE WHEN $2 = 'confirmed' AND confirmed_at IS NULL THEN now() ELSE confirmed_at END
		WHERE id = $1::uuid`, id, status)
	if err != nil {
		return fmt.Errorf("set draft status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Подтверждение черновика ----------

// ConfirmDraft применяет подтверждённые рассказчиком изменения атомарно.
// ИИ не может менять дерево/факты напрямую — только через этот метод.
func (s *Store) ConfirmDraft(ctx context.Context, sessionID string, decisions []domain.DraftDecision, storyText string, storyPersonIDs []string) error {
	draft, err := s.GetDraftBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	if draft.Status == domain.DraftConfirmed {
		return ErrDraftAlreadyConfirmed
	}
	if draft.Status == domain.DraftDismissed {
		return ErrDraftDismissed
	}

	// Приводим решения к удобному виду.
	byItem := map[string]domain.DraftDecision{}
	for _, d := range decisions {
		byItem[d.ItemID] = d
	}

	return s.TxRunner(ctx, func(tx pgx.Tx) error {
		// refMap связывает временные ссылки новых людей ("new:p1") с реальными UUID
		// после их создания — так связи между новыми людьми не требуют Exist-проверки заранее.
		refMap := map[string]string{}
		for _, item := range draft.Items {
			dec, ok := byItem[item.ID]
			if !ok {
				continue // неуказанные пункты остаются pending и не применяются
			}
			if dec.Action == string(domain.DraftDismissed) {
				if _, err := tx.Exec(ctx,
					`UPDATE draft_items SET status = 'dismissed' WHERE id = $1::uuid`, item.ID); err != nil {
					return err
				}
				continue
			}
			payload := item.Payload
			if len(dec.Payload) > 0 {
				payload = dec.Payload
			}
			refID, err := applyDraftItem(ctx, tx, draft.SessionID, item.Kind, item.Action, payload, refMap)
			if err != nil {
				return fmt.Errorf("apply draft item %s (%s): %w", item.ID, item.Kind, err)
			}
			if refID != nil {
				if temp := payloadString(payload, "ref"); strings.HasPrefix(temp, "new:") {
					refMap[temp] = *refID
				}
			}
			if _, err := tx.Exec(ctx,
				`UPDATE draft_items SET status = 'confirmed', resolved_ref_id = $2::uuid WHERE id = $1::uuid`,
				item.ID, uuidPtr(refID)); err != nil {
				return err
			}
		}

		// Текст рассказа: приоритет у решения по story-элементу, затем переданный текст.
		finalText := storyText
		if finalText == "" {
			for _, item := range draft.Items {
				if item.Kind == domain.ItemStory {
					if dec, ok := byItem[item.ID]; ok && dec.Action != string(domain.DraftDismissed) {
						p := item.Payload
						if len(dec.Payload) > 0 {
							p = dec.Payload
						}
						finalText = payloadString(p, "text")
						if ids := payloadStringSlice(p, "person_ids"); len(ids) > 0 {
							storyPersonIDs = ids
						}
					}
				}
			}
		}
		if finalText != "" {
			var storyID string
			if err := tx.QueryRow(ctx, `INSERT INTO stories (session_id, text, status, version)
				VALUES ($1::uuid, $2, 'confirmed', 1) RETURNING id::text`, draft.SessionID, finalText).
				Scan(&storyID); err != nil {
				return fmt.Errorf("create confirmed story: %w", err)
			}
			for _, pid := range storyPersonIDs {
				if _, err := tx.Exec(ctx,
					`INSERT INTO story_persons (story_id, person_id) VALUES ($1::uuid, $2::uuid)
					 ON CONFLICT DO NOTHING`, storyID, pid); err != nil {
					return err
				}
			}
			// Факты, созданные в этом черновике, привязываем к рассказу.
			if _, err := tx.Exec(ctx,
				`UPDATE facts SET story_id = $2::uuid WHERE session_id = $1::uuid AND story_id IS NULL`,
				draft.SessionID, storyID); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(ctx, `UPDATE drafts SET status = 'confirmed',
			confirmed_at = COALESCE(confirmed_at, now()), story_text = $2 WHERE id = $1::uuid`,
			draft.ID, finalText); err != nil {
			return err
		}
		return nil
	})
}

// applyDraftItem создаёт/изменяет сущность по подтверждённому draft_item.
// Возвращает идентификатор затронутой сущности (может быть nil).
func applyDraftItem(ctx context.Context, tx pgx.Tx, sessionID string, kind domain.DraftItemKind,
	action domain.DraftItemAction, p map[string]any, refs map[string]string) (*string, error) {
	switch kind {
	case domain.ItemPerson:
		person := domain.Person{
			DisplayName:  payloadString(p, "display_name"),
			NameVariants: payloadStringSlice(p, "name_variants"),
			BirthDate:    payloadString(p, "birth_date"),
			BirthApprox:  payloadBool(p, "birth_approx"),
			DeathDate:    payloadString(p, "death_date"),
			DeathApprox:  payloadBool(p, "death_approx"),
			Gender:       payloadString(p, "gender"),
			Places:       payloadStringSlice(p, "places"),
			Notes:        payloadString(p, "notes"),
		}
		if v := payloadString(p, "photo_media_id"); v != "" {
			person.PhotoMediaID = &v
		}
		if action == domain.ActionUpdate || action == domain.ActionLink {
			existing := payloadString(p, "id")
			if existing == "" {
				return nil, errors.New("update person: не задан id")
			}
			person.ID = existing
			if err := updatePersonTx(ctx, tx, &person); err != nil {
				return nil, err
			}
			return &person.ID, nil
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO persons
			(display_name, name_variants, birth_date, birth_approx, death_date, death_approx, gender, places, notes, photo_media_id)
			VALUES ($1, $2::text[], $3, $4, $5, $6, $7, $8::text[], $9, $10::uuid) RETURNING id::text`,
			person.DisplayName, strSlice(person.NameVariants), strPtr(person.BirthDate), person.BirthApprox,
			strPtr(person.DeathDate), person.DeathApprox, strPtr(person.Gender), strSlice(person.Places),
			person.Notes, uuidPtr(person.PhotoMediaID)).Scan(&id); err != nil {
			return nil, err
		}
		return &id, nil

	case domain.ItemRelationship:
		fromID := resolveRef(refs, payloadString(p, "from_ref"))
		if fromID == "" {
			fromID = payloadString(p, "from_person_id")
		}
		toID := resolveRef(refs, payloadString(p, "to_ref"))
		if toID == "" {
			toID = payloadString(p, "to_person_id")
		}
		if fromID == "" || toID == "" {
			return nil, errors.New("relationship: не определены участники")
		}
		relType := payloadString(p, "type")
		if relType == "" {
			relType = string(domain.RelOther)
		}
		conf := payloadString(p, "confidence")
		if conf == "" {
			conf = string(domain.ConfidenceMedium)
		}
		note := payloadString(p, "note")
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO relationships
			(from_person_id, to_person_id, type, confidence, source, note)
			VALUES ($1::uuid, $2::uuid, $3, $4, 'ai_draft', $5) RETURNING id::text`,
			fromID, toID, relType, conf, note).Scan(&id); err != nil {
			return nil, err
		}
		return &id, nil

	case domain.ItemFact:
		personID := resolveRef(refs, payloadString(p, "person_ref"))
		if personID == "" {
			personID = payloadString(p, "person_id")
		}
		fact := domain.Fact{
			PersonID:      strPtr(personID),
			SessionID:     &sessionID,
			Type:          payloadString(p, "type"),
			Value:         payloadString(p, "value"),
			Confidence:    float64(payloadNum(p, "confidence")),
			SourceRef:     payloadString(p, "source_ref"),
			SourceExcerpt: payloadString(p, "source_excerpt"),
			Confirmed:     true,
		}
		if fact.Type == "" || fact.Value == "" {
			return nil, errors.New("fact: не заданы type/value")
		}
		norm, err := jsonArg(p["normalized"])
		if err != nil {
			return nil, err
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO facts
			(person_id, session_id, type, value, normalized, confidence, source_ref, source_excerpt, confirmed)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5::jsonb, $6, $7, $8, true) RETURNING id::text`,
			uuidPtr(fact.PersonID), fact.SessionID, fact.Type, fact.Value, norm, fact.Confidence,
			fact.SourceRef, fact.SourceExcerpt).Scan(&id); err != nil {
			return nil, err
		}
		return &id, nil

	case domain.ItemQuestion:
		text := payloadString(p, "text")
		if text == "" {
			return nil, errors.New("question: пустой текст")
		}
		personID := strPtr(resolveRef(refs, payloadString(p, "person_ref")))
		if personID == nil {
			personID = strPtr(payloadString(p, "person_id"))
		}
		reason := payloadString(p, "reason")
		priority := int(payloadNum(p, "priority"))
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO questions (person_id, text, source, reason, status, priority, related_session_id)
			VALUES ($1::uuid, $2, 'extraction', $3, 'open', $4, $5::uuid) RETURNING id::text`,
			personID, text, reason, priority, sessionID).Scan(&id); err != nil {
			return nil, err
		}
		return &id, nil

	case domain.ItemMediaLink:
		personID := resolveRef(refs, payloadString(p, "person_ref"))
		if personID == "" {
			personID = payloadString(p, "person_id")
		}
		mediaID := payloadString(p, "media_id")
		if personID == "" || mediaID == "" {
			return nil, errors.New("media_link: не задан person_id/media_id")
		}
		if _, err := tx.Exec(ctx,
			`UPDATE persons SET photo_media_id = $2::uuid WHERE id = $1::uuid`, personID, mediaID); err != nil {
			return nil, err
		}
		return &personID, nil

	case domain.ItemStory:
		// Создаётся на уровне черновика (см. ConfirmDraft): текст может быть отредактирован рассказчиком.
		return nil, nil
	}
	return nil, fmt.Errorf("неизвестный kind draft_item: %s", kind)
}

func updatePersonTx(ctx context.Context, tx pgx.Tx, p *domain.Person) error {
	tag, err := tx.Exec(ctx, `UPDATE persons SET
		display_name = $2, name_variants = $3::text[], birth_date = $4, birth_approx = $5,
		death_date = $6, death_approx = $7, gender = $8, places = $9::text[], notes = $10,
		photo_media_id = $11::uuid WHERE id = $1::uuid`,
		p.ID, strSlice(p.NameVariants), strPtr(p.BirthDate), p.BirthApprox, strPtr(p.DeathDate),
		p.DeathApprox, strPtr(p.Gender), strSlice(p.Places), p.Notes, uuidPtr(p.PhotoMediaID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var (
	ErrDraftAlreadyConfirmed = errors.New("черновик уже подтверждён")
	ErrDraftDismissed        = errors.New("черновик отклонён")
)

// ---------- payload helpers ----------

func payloadString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

func payloadBool(p map[string]any, key string) bool {
	if p == nil {
		return false
	}
	if v, ok := p[key].(bool); ok {
		return v
	}
	return false
}

func payloadNum(p map[string]any, key string) float64 {
	if p == nil {
		return 0
	}
	switch v := p[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func payloadStringSlice(p map[string]any, key string) []string {
	if p == nil {
		return nil
	}
	raw, ok := p[key].([]any)
	if !ok {
		if v, ok := p[key].([]string); ok {
			return v
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// resolveRef превращает временную ссылку "new:p1" в реальный UUID,
// а обычный UUID возвращает как есть.
func resolveRef(refs map[string]string, v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "new:") {
		return refs[v]
	}
	return v
}

func strSlice(in []string) any {
	if in == nil {
		return []string{}
	}
	return in
}
