# Memory Button API

REST API (JSON), базовый префикс `/api/v1`.  
Интерактивная спецификация: **Swagger UI** — [`/docs/`](../docs/) (на развёрнутом инстансе), OpenAPI — [`/openapi.yaml`](../openapi.yaml).

## Авторизация
- `POST /api/v1/auth/login` — `{ "username", "password" }` → `{ token, token_type: "Bearer", expires_in, user }`
- Все защищённые эндпоинты: заголовок `Authorization: Bearer <token>`
- `GET /api/v1/me` — текущий пользователь

Single-user (одна семья). Опционально поддерживается статический bearer (`AUTH_STATIC_TOKEN`) для отладки клиента.

## Основные ресурсы

### Люди и связи
- `GET/POST /api/v1/persons`, `GET/PATCH /api/v1/persons/{id}` (карточка: person, relationships, stories, facts, questions)
- `POST /api/v1/persons/{id}/photo` — multipart `file`
- `GET/POST /api/v1/relationships`, `PATCH/DELETE /api/v1/relationships/{id}`  
  Типы: `parent_child | spouse | sibling | other`

### Сессии (turn-by-turn, строго по очереди)
- `POST /api/v1/sessions` — `{ mode: "new_story|continue_person|clarify_person|qa", person_id?, question_ids? }` → session, приветствие (текст/аудио), `known_facts`, `open_questions`
- `GET /api/v1/sessions/{id}` — сессия и реплики
- `POST /api/v1/sessions/{id}/turns` — `multipart/form-data` поле `audio` → `{ recognized_text, reply{ text, audio_url, ... }, user_turn, status }`
- `POST /api/v1/sessions/{id}/complete` — завершение, запуск постобработки
- `GET /api/v1/sessions/{id}/draft` — черновик (рассказ, предложенные люди/связи/факты/вопросы)

### Подтверждение (только по явному решению рассказчика)
- `POST /api/v1/drafts/{session_id}/confirm` — батч решений `confirm|dismiss` по `items[]` с опциональными правками (`payload`), плюс итоговый `story_text`/`story_person_ids`. Применяется **только подтверждённое**.
- `POST /api/v1/stories/{id}/confirm`, `GET /api/v1/stories/{id}`
- `GET /api/v1/persons/{id}/stories`

### Реестр вопросов
- `GET /api/v1/questions?person_id=&status=open|resolved|dismissed&source=`
- `POST /api/v1/questions`, `PATCH /api/v1/questions/{id}`
- `GET /api/v1/persons/{id}/questions`

Статусы: `open | resolved | dismissed`. Источники: `dialogue | qa | extraction | manual`.

### Q&A
- `POST /api/v1/qa/ask` — `{ question, scope_person_id? }` → `{ answer, sources[{person_id,person_name,story_id,fact_id,excerpt}], insufficient_data, suggested_questions[] }`  
  Ответы только по **подтверждённым** данным; при нехватке — `insufficient_data=true`.

### Медиа
- `POST /api/v1/media/upload` (multipart), `POST /api/v1/media/presign`
- `GET /api/v1/media/{id}`, `GET /api/v1/media/{id}/content`

### Служебные
- `GET /health`, `GET /api/v1/health`

## Ошибки
Формат: `{ "error": "сообщение", "code": "код", "request_id": "..." }`.  
Типичные коды: `invalid_request`/`validation_error`, `unauthorized`, `invalid_credentials`, `not_found`, `session_not_active`, `draft_confirmed`, `conflict`, `internal`.

## Замечания
- Голосовой пайплайн и постобработка используют HF или заглушки (`AI_PROVIDER=stub|hf`, `AI_FALLBACK_TO_STUB`); в ответах/метаданных может быть `fallback=true`.
- Черновики не меняют дерево/связи до подтверждения — это инвариант MVP.
- Полные схемы запросов/ответов — в [`openapi.yaml`](../openapi.yaml).
