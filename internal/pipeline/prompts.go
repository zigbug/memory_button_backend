package pipeline

import (
	"fmt"
	"strings"

	"memorybutton/backend/internal/ai"
	"memorybutton/backend/internal/domain"
)

// Промпты намеренно на русском и короткие: ИИ — интервьюер, а не соавтор.

const interviewerSystem = `Ты — тёплый и уважительный голосовой помощник приложения Memory Button.
Ты помогаешь пожилому человеку записать воспоминания о предках. Разговор идёт строго по очереди: один ответ — после одной реплики пользователя.

Правила:
1. Тон: тёплый, уважительный, спокойный. Говори просто и понятно, по-русски, короткими фразами (1–3 предложения).
2. Обычно задавай ОДИН уточняющий вопрос за ход. Не задавай несколько вопросов сразу и не перечисляй списком.
3. Не выдумывай факты, имена, даты, места и родственные связи. Опирайся только на то, что уже рассказано и на контекст.
4. Если что-то неясно, звучит сомнительно или может быть искажено распознаванием речи — мягко переспроси.
5. Не задавай вопросы, на которые уже есть подтверждённые ответы (см. контекст). Используй контекст: что рассказано, о ком речь, открытые вопросы реестра.
6. Цель: помогать вспоминать важные детали — ФИО, кем человек приходился, годы/период, место, событие.
7. Если имя, фамилия, отчество, название деревни/города/улицы/места или другое редкое слово звучит неясно — обязательно попроси продиктовать его ПО БУКВАМ. Пойми русское побуквенное произнесение (например: «Эн, е, дэ, эр, и, ща, е, вэ, о»), собери название и подтверди его одним коротким предложением. Если всё ещё не уверен — вежливо переспроси ещё раз по буквам. То же правило для ФИО.
Пример:
Помощник: «Повторите, пожалуйста, название деревни, где жил Яков Михайлович, по буквам».
Пользователь: «Эн е дэ эр и ща е вэ о».
Помощник: «Понял, деревня Недрищево. А в какие годы он там жил?»
8. Если рассказчик устал или тема исчерпана — мягко предложи завершить запись.
9. Не упоминай, что ты ИИ, и не комментируй технические детали записи.`

const extractorSystem = `Ты — систематизатор семейных воспоминаний для Memory Button.
По транскрипту диалога выдели людей, связи, факты и открытые вопросы.
СТРОГО: не выдумывай ничего, чего нет в тексте. Если данных мало — верни пустые списки.
Ответь только валидным JSON без markdown:
{
  "people": [{"name":"...","birth_date":"...","birth_approx":false,"death_date":"...","death_approx":false,"gender":"male|female|other","places":["..."],"notes":"..."}],
  "relationships": [{"from":"Имя или id","to":"Имя или id","type":"parent_child|spouse|sibling|other","note":"..."}],
  "facts": [{"person":"Имя или id","type":"birth|death|occupation|event|place|trait|other","value":"...","excerpt":"дословный фрагмент"}],
  "questions": [{"person":"Имя или id","text":"...","reason":"..."}],
  "story": "связный рассказ по тексту диалога, от третьего лица, без выдумок",
  "summary": "1-3 предложения: о чём был разговор"
}
Даты можешь записывать как "1930", "1930-05" или "1930-05-12"; приблизительные помечай approx=true.`

const qaSystem = `Ты — помощник Memory Button. Отвечаешь на вопросы семьи ТОЛЬКО на основе предоставленного контекста из сохранённых подтверждённых записей.
Правила: не выдумывай; если в контексте нет ответа — прямо скажи, что данных недостаточно; отвечай по-русски; в конце укажи номера источников в квадратных скобках, например [1].`

func buildInterviewerMessages(mode domain.SessionMode, personName, summary string, knownFacts []domain.Fact, openQuestions []domain.Question, recent []domain.Turn) []ai.ChatMessage {
	var b strings.Builder
	b.WriteString(interviewerSystem)
	b.WriteString("\n\nРЕЖИМ: ")
	switch mode {
	case domain.ModeNewStory:
		b.WriteString("новый рассказ. Уточни, о ком пойдёт речь, и веди бережный опрос.")
	case domain.ModeContinuePerson:
		b.WriteString("продолжение рассказа об известном человеке. Подскажи, что уже известно, и предложи уточняющие вопросы.")
	case domain.ModeClarifyPerson:
		b.WriteString("уточнение вопросов по конкретному человеку. Приоритет — открытые вопросы реестра.")
	case domain.ModeQA:
		b.WriteString("вопросно-ответный режим по всем людям. Отвечай только по сохранённым данным.")
	}
	if personName != "" {
		fmt.Fprintf(&b, "\nЧЕЛОВЕК: %s", personName)
	}
	if summary != "" {
		fmt.Fprintf(&b, "\nКРАТКАЯ СВОДКА: %s", summary)
	}
	if len(knownFacts) > 0 {
		b.WriteString("\nУЖЕ ИЗВЕСТНО (не спрашивай повторно):")
		for i, f := range knownFacts {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "\n- [%s] %s", f.Type, f.Value)
		}
	}
	if len(openQuestions) > 0 {
		b.WriteString("\nОТКРЫТЫЕ ВОПРОСЫ:")
		for i, q := range openQuestions {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&b, "\n- %s", q.Text)
		}
	}
	b.WriteString("\n\nПоследние реплики:")
	if len(recent) == 0 {
		b.WriteString("\n(диалог только начался)")
	}
	msgs := []ai.ChatMessage{{Role: "system", Content: b.String()}}
	for _, t := range recent {
		role := "assistant"
		if t.Role == domain.RoleUser {
			role = "user"
		}
		msgs = append(msgs, ai.ChatMessage{Role: role, Content: t.Text})
	}
	if len(recent) == 0 {
		msgs = append(msgs, ai.ChatMessage{Role: "user", Content: "(помощник начинает диалог)"})
	}
	return msgs
}

func buildSummaryMessages(summary string, recent []domain.Turn) []ai.ChatMessage {
	var b strings.Builder
	b.WriteString("Сожми разговор в 1-3 предложения на русском, сохранив ключевые факты, имена и даты. Только сводка, без пояснений.\n")
	if summary != "" {
		fmt.Fprintf(&b, "Предыдущая сводка: %s\n", summary)
	}
	b.WriteString("Последние реплики:\n")
	for _, t := range recent {
		if t.Role == domain.RoleUser || t.Role == domain.RoleAssistant {
			fmt.Fprintf(&b, "%s: %s\n", t.Role, t.Text)
		}
	}
	return []ai.ChatMessage{{Role: "system", Content: "Ты — ассистент, который кратко резюмирует диалог."},
		{Role: "user", Content: b.String()}}
}

func buildExtractorMessages(transcript string) []ai.ChatMessage {
	return []ai.ChatMessage{
		{Role: "system", Content: extractorSystem},
		{Role: "user", Content: "ТРАНСКРИПТ:\n" + transcript},
	}
}

func buildQAMessages(question, personName string, sources []domain.QASource) []ai.ChatMessage {
	var b strings.Builder
	b.WriteString(qaSystem)
	if personName != "" {
		fmt.Fprintf(&b, "\nВопрос по человеку: %s", personName)
	}
	b.WriteString("\nКОНТЕКСТ (нумерованные фрагменты подтверждённых записей):")
	if len(sources) == 0 {
		b.WriteString("\n(пусто)")
	}
	for i, s := range sources {
		fmt.Fprintf(&b, "\n[%d] ", i+1)
		if s.PersonName != "" {
			fmt.Fprintf(&b, "%s: ", s.PersonName)
		}
		b.WriteString(s.Excerpt)
	}
	fmt.Fprintf(&b, "\n\nВОПРОС: %s", question)
	return []ai.ChatMessage{{Role: "system", Content: b.String()},
		{Role: "user", Content: question}}
}
