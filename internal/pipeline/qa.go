package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/domain"
	"memorybutton/backend/internal/storage/postgres"
)

type QAService struct {
	store  *postgres.Store
	ai     ai.Provider
	logger *slog.Logger
}

func NewQAService(store *postgres.Store, provider ai.Provider, logger *slog.Logger) *QAService {
	return &QAService{store: store, ai: provider, logger: logger}
}

// Ask отвечает на вопрос только по подтверждённым сохранённым данным.
// При нехватке данных честно возвращает insufficient_data=true и предлагает
// вопросы из реестра (ничего не выдумывая).
func (q *QAService) Ask(ctx context.Context, question string, scopePersonID *string) (*domain.QAAnswer, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("пустой вопрос")
	}

	personID := deref(scopePersonID)
	personName := ""
	if personID != "" {
		if name, err := q.store.PersonName(ctx, personID); err == nil {
			personName = name
		}
	}

	sources, err := q.retrieve(ctx, personID, question)
	if err != nil {
		return nil, err
	}

	// Вопросы из реестра для подсказки.
	openQs, _ := q.store.OpenQuestions(ctx, personID, 5)
	suggested := make([]string, 0, len(openQs)+1)
	for _, item := range openQs {
		suggested = append(suggested, item.Text)
	}

	if len(sources) == 0 {
		answer := "В сохранённых подтверждённых записях пока нет информации, чтобы ответить на этот вопрос. " +
			"Вы можете добавить его в реестр открытых вопросов и записать ответ в диалоге с помощником."
		return &domain.QAAnswer{
			Answer:             answer,
			Sources:            []domain.QASource{},
			InsufficientData:   true,
			SuggestedQuestions: suggested,
		}, nil
	}

	// Если вопрос сам по себе может стать полезным открытым — предлагаем его.
	if !containsFold(suggested, question) {
		suggested = append(suggested, question)
	}

	chatRes, err := q.ai.LLM.Chat(ctx, ai.ChatRequest{
		Messages: buildQAMessages(question, personName, sources),
		Purpose:  ai.PurposeQA,
		MaxTokens: 400,
	})
	if err != nil {
		// graceful fallback: отвечаем по первому источнику без генерации.
		q.logger.Warn("LLM Q&A недоступен, отвечаем по источнику", "err", err)
		return &domain.QAAnswer{
			Answer:             "Согласно сохранённой записи: " + sources[0].Excerpt,
			Sources:            sources,
			InsufficientData:   false,
			SuggestedQuestions: suggested,
		}, nil
	}
	return &domain.QAAnswer{
		Answer:             chatRes.Text,
		Sources:            sources,
		InsufficientData:   false,
		SuggestedQuestions: suggested,
	}, nil
}

// retrieve собирает подтверждённые факты и рассказы как источники ответа.
func (q *QAService) retrieve(ctx context.Context, personID, question string) ([]domain.QASource, error) {
	sources := []domain.QASource{}

	facts, err := q.store.SearchConfirmedFacts(ctx, personID, question, 30)
	if err != nil {
		return nil, err
	}
	nameCache := map[string]string{}
	for _, f := range facts {
		name := ""
		if f.PersonID != nil {
			name = nameCache[*f.PersonID]
			if name == "" {
				if n, err := q.store.PersonName(ctx, *f.PersonID); err == nil {
					name = n
					nameCache[*f.PersonID] = n
				}
			}
		}
		excerpt := f.SourceExcerpt
		if excerpt == "" {
			excerpt = f.Value
		}
		sources = append(sources, domain.QASource{
			PersonID:   deref(f.PersonID),
			PersonName: name,
			FactID:     f.ID,
			Excerpt:    excerpt,
		})
	}

	var stories []domain.Story
	if personID != "" {
		stories, err = q.store.ListStoriesByPerson(ctx, personID)
	} else {
		stories, err = q.store.ListConfirmedStories(ctx, 10)
	}
	if err != nil {
		return nil, err
	}
	for _, st := range stories {
		if st.Status != domain.StoryConfirmed {
			continue
		}
		excerpt := snippet(st.Text, question, 220)
		if excerpt == "" {
			continue
		}
		src := domain.QASource{StoryID: st.ID, Excerpt: excerpt}
		if len(st.PersonIDs) > 0 {
			src.PersonID = st.PersonIDs[0]
			if n, err := q.store.PersonName(ctx, st.PersonIDs[0]); err == nil {
				src.PersonName = n
			}
		}
		sources = append(sources, src)
	}
	return sources, nil
}

// snippet вырезает фрагмент текста вокруг первого вхождения ключевого слова.
func snippet(text, query string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) <= max {
		return text
	}
	lower := strings.ToLower(text)
	q := strings.ToLower(query)
	if idx := strings.Index(lower, firstWord(q)); idx >= 0 {
		start := idx - max/3
		if start < 0 {
			start = 0
		}
		end := start + max
		if end > len(text) {
			end = len(text)
			start = end - max
			if start < 0 {
				start = 0
			}
		}
		out := text[start:end]
		if start > 0 {
			out = "…" + out
		}
		if end < len(text) {
			out += "…"
		}
		return out
	}
	if len(text) > max {
		return text[:max] + "…"
	}
	return text
}

func firstWord(q string) string {
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return q
	}
	return fields[0]
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}
