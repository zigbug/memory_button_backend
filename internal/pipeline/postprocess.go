package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/domain"
	"memorybutton/backend/internal/storage/media"
	"memorybutton/backend/internal/storage/postgres"
)

type Postprocessor struct {
	store           *postgres.Store
	ai              ai.Provider
	media           *media.Service
	logger          *slog.Logger
	interviewerModel string
	extractorModel   string
}

func NewPostprocessor(store *postgres.Store, provider ai.Provider, mediaSvc *media.Service, logger *slog.Logger,
	interviewerModel, extractorModel string) *Postprocessor {
	return &Postprocessor{
		store: store, ai: provider, media: mediaSvc, logger: logger,
		interviewerModel: interviewerModel, extractorModel: extractorModel,
	}
}

// ---------- Структуры ответа экстрактора ----------

type Extraction struct {
	People        []ExtractedPerson  `json:"people"`
	Relationships []ExtractedRelation `json:"relationships"`
	Facts         []ExtractedFact    `json:"facts"`
	Questions     []ExtractedQuestion `json:"questions"`
	Story         string             `json:"story"`
	Summary       string             `json:"summary"`
}

type ExtractedPerson struct {
	Name       string   `json:"name"`
	BirthDate  string   `json:"birth_date"`
	BirthApprox bool    `json:"birth_approx"`
	DeathDate  string   `json:"death_date"`
	DeathApprox bool    `json:"death_approx"`
	Gender     string   `json:"gender"`
	Places     []string `json:"places"`
	Notes      string   `json:"notes"`
}

type ExtractedRelation struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
	Note string `json:"note"`
}

type ExtractedFact struct {
	Person  string `json:"person"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Excerpt string `json:"excerpt"`
}

type ExtractedQuestion struct {
	Person string `json:"person"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

// ProcessSession строит черновик (рассказ + предложенные факты/люди/связи/вопросы).
// Ничего не записывает в основные таблицы — только drafts/draft_items.
func (p *Postprocessor) ProcessSession(ctx context.Context, sessionID string) (*domain.Draft, error) {
	turns, err := p.store.ListTurns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, errors.New("в сессии нет реплик")
	}
	transcript := buildTranscript(turns)

	extraction, engine, fallback, err := p.extract(ctx, transcript)
	if err != nil {
		p.logger.Warn("экстрактор недоступен, используем эвристику", "session_id", sessionID, "err", err)
		extraction = heuristicExtraction(turns)
		fallback = true
	}
	if extraction.Summary == "" {
		extraction.Summary = firstUserText(turns)
	}
	story := strings.TrimSpace(extraction.Story)
	if story == "" {
		story = rawStory(turns)
	}

	draft := &domain.Draft{
		SessionID: sessionID,
		Status:    domain.DraftPending,
		StoryText: story,
		Meta: map[string]any{
			"engine":          engine,
			"fallback":        fallback,
			"turns":           len(turns),
			"transcript_chars": len(transcript),
		},
	}
	if err := p.store.CreateDraft(ctx, draft); err != nil {
		return nil, err
	}

	if err := p.buildItems(ctx, draft, extraction); err != nil {
		return nil, err
	}

	// Сводка сессии — по итогам экстракции.
	if extraction.Summary != "" {
		_ = p.store.UpdateSessionSummary(ctx, sessionID, extraction.Summary)
	}

	// Транскрипт сохраняем для аудита/будущего уточнения таймкодов.
	segments := make([]domain.TranscriptSegment, 0, len(turns))
	for _, t := range turns {
		if t.Role == domain.RoleSystem {
			continue
		}
		segments = append(segments, domain.TranscriptSegment{Text: t.Text, Speaker: string(t.Role)})
	}
	_ = p.store.SaveTranscript(ctx, &domain.Transcript{
		SessionID: sessionID, Segments: segments, Engine: engine,
	})

	full, err := p.store.GetDraft(ctx, draft.ID)
	if err != nil {
		return draft, nil
	}
	p.logger.Info("черновик готов", "session_id", sessionID, "draft_id", draft.ID,
		"items", len(full.Items), "fallback", fallback)
	return &full, nil
}

func (p *Postprocessor) extract(ctx context.Context, transcript string) (*Extraction, string, bool, error) {
	res, err := p.ai.LLM.Chat(ctx, ai.ChatRequest{
		Model:    p.extractorModel,
		Messages: buildExtractorMessages(transcript),
		Purpose:  ai.PurposeExtract,
		JSONMode: true,
	})
	if err != nil {
		return nil, "", false, err
	}
	text := extractJSON(res.Text)
	var out Extraction
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, res.Model, res.Fallback, fmt.Errorf("json экстрактора: %w", err)
	}
	return &out, res.Model, res.Fallback, nil
}

// buildItems превращает результат экстракции в draft_items.
func (p *Postprocessor) buildItems(ctx context.Context, draft *domain.Draft, ex *Extraction) error {
	// Собираем все имена, чтобы сопоставить с уже существующими людьми.
	names := map[string]bool{}
	for _, person := range ex.People {
		names[normName(person.Name)] = true
	}
	for _, rel := range ex.Relationships {
		names[normName(rel.From)] = true
		names[normName(rel.To)] = true
	}
	for _, f := range ex.Facts {
		if f.Person != "" {
			names[normName(f.Person)] = true
		}
	}
	for _, q := range ex.Questions {
		if q.Person != "" {
			names[normName(q.Person)] = true
		}
	}
	existing, err := p.store.SearchPersonsByNames(ctx, keys(names))
	if err != nil {
		return err
	}

	// name -> ref ("new:pN") или существующий person_id.
	resolved := map[string]string{}
	newCount := 0
	for name := range names {
		if name == "" {
			continue
		}
		if person, ok := existing[name]; ok {
			resolved[name] = person.ID
			continue
		}
		newCount++
		resolved[name] = fmt.Sprintf("new:p%d", newCount)
	}

	// Новые люди.
	for _, person := range ex.People {
		name := normName(person.Name)
		ref, ok := resolved[name]
		if !ok || !strings.HasPrefix(ref, "new:") {
			continue // существующего человека не трогаем
		}
		payload := map[string]any{
			"ref":           ref,
			"display_name":  person.Name,
			"name_variants": []string{},
			"birth_date":    person.BirthDate,
			"birth_approx":  person.BirthApprox,
			"death_date":    person.DeathDate,
			"death_approx":  person.DeathApprox,
			"gender":        person.Gender,
			"places":        nonEmpty(person.Places),
			"notes":         person.Notes,
		}
		if err := p.addItem(ctx, draft.ID, domain.ItemPerson, domain.ActionCreate, payload); err != nil {
			return err
		}
	}

	// Связи.
	for _, rel := range ex.Relationships {
		fromRef, ok1 := resolved[normName(rel.From)]
		toRef, ok2 := resolved[normName(rel.To)]
		if !ok1 || !ok2 || fromRef == toRef {
			continue
		}
		relType := domain.RelationshipType(rel.Type)
		switch relType {
		case domain.RelParentChild, domain.RelSpouse, domain.RelSibling, domain.RelOther:
		default:
			relType = domain.RelOther
		}
		payload := map[string]any{
			"from_ref": fromRef,
			"to_ref":   toRef,
			"type":     relType,
			"note":     rel.Note,
		}
		if err := p.addItem(ctx, draft.ID, domain.ItemRelationship, domain.ActionCreate, payload); err != nil {
			return err
		}
	}

	// Факты.
	for _, f := range ex.Facts {
		personRef := resolved[normName(f.Person)]
		if f.Value == "" {
			continue
		}
		payload := map[string]any{
			"person_ref":    personRef,
			"type":          f.Type,
			"value":         f.Value,
			"excerpt":       f.Excerpt,
			"source_excerpt": f.Excerpt,
			"confidence":    0.8,
		}
		if err := p.addItem(ctx, draft.ID, domain.ItemFact, domain.ActionCreate, payload); err != nil {
			return err
		}
	}

	// Вопросы.
	for _, q := range ex.Questions {
		if strings.TrimSpace(q.Text) == "" {
			continue
		}
		payload := map[string]any{
			"person_ref": resolved[normName(q.Person)],
			"text":       strings.TrimSpace(q.Text),
			"reason":     q.Reason,
			"priority":   0,
		}
		if err := p.addItem(ctx, draft.ID, domain.ItemQuestion, domain.ActionCreate, payload); err != nil {
			return err
		}
	}

	// Элемент рассказа — чтобы рассказчик мог отредактировать текст при подтверждении.
	if draft.StoryText != "" {
		if err := p.addItem(ctx, draft.ID, domain.ItemStory, domain.ActionCreate,
			map[string]any{"text": draft.StoryText}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postprocessor) addItem(ctx context.Context, draftID string, kind domain.DraftItemKind,
	action domain.DraftItemAction, payload map[string]any) error {
	item := &domain.DraftItem{
		DraftID: draftID,
		Kind:    kind,
		Action:  action,
		Payload: payload,
		Status:  domain.DraftPending,
	}
	return p.store.AddDraftItem(ctx, item)
}

// ---------- fallback ----------

func heuristicExtraction(turns []domain.Turn) *Extraction {
	return &Extraction{
		Questions: []ExtractedQuestion{{
			Text:   "Что ещё важно запомнить об этом человеке?",
			Reason: "заполнительный вопрос при недоступности ИИ",
		}},
	}
}

func rawStory(turns []domain.Turn) string {
	var b strings.Builder
	for _, t := range turns {
		if t.Role != domain.RoleUser {
			continue
		}
		b.WriteString(t.Text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func firstUserText(turns []domain.Turn) string {
	for _, t := range turns {
		if t.Role == domain.RoleUser && t.Text != "" {
			return t.Text
		}
	}
	return ""
}

func buildTranscript(turns []domain.Turn) string {
	var b strings.Builder
	for _, t := range turns {
		switch t.Role {
		case domain.RoleUser:
			fmt.Fprintf(&b, "Рассказчик: %s\n", t.Text)
		case domain.RoleAssistant:
			fmt.Fprintf(&b, "Помощник: %s\n", t.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// extractJSON вырезает JSON-объект из ответа, если модель обернула его в текст.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func normName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func nonEmpty(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return in
}
