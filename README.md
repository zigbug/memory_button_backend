# Memory Button — Backend (Go, MVP)

Базовый бэкенд для Android-приложения **Memory Button** («голосовые воспоминания о предках»).  
Один пользователь / одна семья, основной язык — русский. Клиент (Flutter) ведёт диалог с ИИ строго по очереди: запись → обработка → ответ.

## Возможности MVP

- **Авторизация single-user**: логин/пароль → JWT (Bearer). Опционально статический bearer-токен для отладки.
- **Люди и связи**: карточка человека (ФИО/варианты, годы, в т.ч. приблизительные, заметки, фото, связанные рассказы/факты/вопросы), типы связей `parent_child|spouse|sibling|other`.
- **Сессии turn-by-turn**: режимы `new_story | continue_person | clarify_person | qa`. Стартовая реплика помощника с подсказкой «что известно / открытые вопросы», приём аудио, пайплайн ASR → LLM → TTS, возврат текста и аудио ответа.
- **Постобработка**: связный рассказ + извлечение людей/связей/фактов + генерация открытых вопросов. Результат сохраняется как **черновик** (draft).
- **Подтверждение рассказчиком**: факты/привязки/связи/вопросы/рассказ попадают в основные таблицы **только после явного подтверждения** (`/drafts/{session_id}/confirm` или `/stories/{id}/confirm`). ИИ не меняет дерево молча.
- **Реестр вопросов**: `open|resolved|dismissed`, общий и по `person_id`, пополняется вручную/из диалога/экстракции/Q&A; используется при продолжении разговора.
- **Q&A по всем людям**: ответы только на основе **сохранённых подтверждённых** данных, с источниками (`person_id/story_id/fact_id/excerpt`). При нехватке данных — `insufficient_data=true`, честный ответ и предложение добавить вопрос в реестр.
- **Хранилище**: PostgreSQL + S3-совместимое object storage (MinIO). Миграции применяются автоматически при старте.
- **ИИ**: Hugging Face Inference (ASR/LLM/TTS) или детерминированные заглушки; при сбое HF — graceful fallback с пометкой `fallback=true`.

## Структура проекта

```
cmd/api/                 # точка входа HTTP API
internal/
  config/                # конфигурация из env
  domain/                # сущности и типы предметной области
  auth/                  # bcrypt + JWT (HS256), single-user
  ai/
    interfaces.go        # контракты ASR / LLM / TTS
    hf/                  # реализация Hugging Face Inference
    stub/                # заглушки (локальная разработка без ключей)
    fallback.go          # graceful fallback с пометкой fallback
    providerfactory/     # сборка провайдера из конфигурации
  pipeline/
    dialogue.go          # оркестратор сессии (start/turn/complete)
    postprocess.go       # экстрактор черновика (рассказ, факты, вопросы)
    qa.go                # Q&A по подтверждённым данным + источники
    prompts.go           # промпты интервьюера/экстрактора/QA (ru)
  storage/
    postgres/            # пул, миграции, репозитории, confirm draft
    s3/                  # интерфейс + MinIO + in-memory fake
    media/               # связка object storage ↔ таблица media
  http/
    router.go            # маршруты /api/v1
    handlers/            # HTTP-обработчики и DTO
    middleware/          # request-id, логирование, recovery, auth
  httputil/              # JSON/ошибки/контекст
  worker/                # фоновые задачи (outbox jobs в PostgreSQL)
migrations/              # SQL-миграции (embed в бинарник)
deploy/, docs/
```

## Быстрый старт (Docker Compose)

Требуется Docker и Docker Compose.

```bash
cd memory-button-backend
cp .env.example .env
# обязательно смените JWT_SECRET и AUTH_PASSWORD в .env

docker compose up --build
```

Сервисы:
- API: `http://localhost:8080` (`/health`, `/api/v1/...`)
- PostgreSQL: `localhost:5432` (user/pass/db: `memory` / `memory` / `memorybutton`)
- MinIO: API `localhost:9000`, консоль `http://localhost:9001` (minioadmin/minioadmin)

По умолчанию `AI_PROVIDER=stub` — Hugging Face не требуется, ответы помечены как заглушки/fallback там, где это уместно.

## Локальный запуск без Docker

```bash
# зависимости: PostgreSQL, MinIO (или S3-совместимое хранилище)
export PATH=/usr/local/go/bin:$PATH
cp .env.example .env
# настройте DATABASE_URL, S3_*, JWT_SECRET, AUTH_PASSWORD

go build -o memory-button-api ./cmd/api
./memory-button-api
```

Миграции и single-user аккаунт создаются автоматически при старте.

## Конфигурация (env)

| Переменная | По умолчанию | Описание |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `DATABASE_URL` | `postgres://memory:memory@localhost:5432/memorybutton?sslmode=disable` | DSN PostgreSQL |
| `DB_MAX_CONNS` | `10` | Макс. соединений пула |
| `AUTH_USERNAME` | `family` | Логин единственного пользователя |
| `AUTH_PASSWORD` | `change-me` | Пароль (для локальной разработки) |
| `AUTH_PASSWORD_HASH` | — | bcrypt-хеш (приоритет над паролем) |
| `AUTH_STATIC_TOKEN` | — | Опциональный долгоживущий Bearer для отладки |
| `JWT_SECRET` | dev-secret | Секрет HS256 (**обязательно сменить**) |
| `JWT_TTL` | `24h` | Время жизни токена |
| `S3_ENDPOINT` | `localhost:9000` | Хост:порт S3/MinIO |
| `S3_BUCKET` | `memory-button` | Бакет (создаётся при старте) |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | `minioadmin` | Ключи доступа |
| `S3_USE_SSL` | `false` | TLS к S3 |
| `S3_PUBLIC_URL` | — | Базовый URL для прямых ссылок (опц.) |
| `AI_PROVIDER` | `stub` | `stub` или `hf` |
| `AI_FALLBACK_TO_STUB` | `true` | При ошибке HF использовать заглушку |
| `HF_TOKEN` | — | Токен Hugging Face (нужен для `hf`) |
| `HF_BASE_URL` | HF Inference Router | Базовый URL чата |
| `HF_CHAT_MODEL` | `mistralai/Mistral-7B-Instruct-v0.3` | Модель интервьюера/экстрактора |
| `HF_ASR_MODEL` | `openai/whisper-large-v3` | Модель распознавания речи |
| `HF_TTS_MODEL` | `microsoft/speecht5_tts` | Модель синтеза речи |
| `HF_INTERVIEWER_MODEL` / `HF_EXTRACTOR_MODEL` | — | Отдельные модели (опц.) |
| `HF_TIMEOUT` | `90s` | Таймаут вызовов HF |
| `WORKER_ENABLED` | `true` | Фоновый воркер постобработки |
| `WORKER_POLL_INTERVAL` | `2s` | Интервал опроса очереди jobs |
| `PROCESSING_SYNC` | `false` | Синхронная постобработка в `/complete` |

Секреты не хардкодятся; дефолты предназначены только для локальной разработки и логируются предупреждением.

## Авторизация (выбор для single-user)

Используется **bcrypt + JWT HS256**:
- один аккаунт создаётся при старте (`AUTH_USERNAME` + `AUTH_PASSWORD`/`AUTH_PASSWORD_HASH`);
- `POST /api/v1/auth/login` возвращает `access token` (`Bearer`);
- все остальные `/api/v1/*` (кроме `/health` и `/login`) требуют `Authorization: Bearer <token>`;
- опционально `AUTH_STATIC_TOKEN` — долгоживущий bearer для отладки (проверяется на сервере).

Для одной семьи этого достаточно и безопаснее, чем cookie-сессии без TLS-терминации на стороне клиента; в продакшене обязательно HTTPS и сильный `JWT_SECRET`.

## Загрузка медиа (выбор)

Основной режим — **multipart upload через API** (`POST /persons/{id}/photo`, `POST /sessions/{id}/turns` с полем `audio`, `POST /media/upload`).  
Плюсы: единая проверка авторизации/лимитов, простота для Flutter, сервер сам регистрирует запись в `media` и связывает с владельцем.  
Альтернатива: `POST /api/v1/media/presign` возвращает presigned PUT (можно использовать позже для прямой загрузки в S3 без проксирования байтов через API).

## API (черновик / реализованный каркас)

Базовый префикс: `/api/v1`. Все ответы — JSON (кроме `/media/{id}/content`). Ошибки: `{ "error": "...", "code": "...", "request_id": "..." }`.

### Health / Auth
- `GET /health`, `GET /api/v1/health` — состояние (DB, провайдер ИИ)
- `POST /api/v1/auth/login` — `{ "username", "password" }` → `{ token, token_type, expires_in, user }`
- `GET /api/v1/me` — текущий пользователь

### Люди
- `GET /api/v1/persons?search=&limit=&offset=`
- `POST /api/v1/persons`
- `GET /api/v1/persons/{id}` — карточка: `person`, `relationships`, `stories`, `facts` (confirmed), `questions`
- `PATCH /api/v1/persons/{id}`
- `POST /api/v1/persons/{id}/photo` — multipart `file`
- `GET /api/v1/persons/{id}/stories`
- `GET /api/v1/persons/{id}/questions?status=`

### Связи
- `GET /api/v1/relationships?person_id=`
- `POST /api/v1/relationships`
- `PATCH /api/v1/relationships/{id}`
- `DELETE /api/v1/relationships/{id}`

Тело связи: `{ "from_person_id", "to_person_id", "type": "parent_child|spouse|sibling|other", "confidence", "note" }`.

### Сессии (turn-by-turn)
- `POST /api/v1/sessions` — `{ "mode": "new_story|continue_person|clarify_person|qa", "person_id": "uuid?", "question_ids": [] }`  
  Ответ: `{ session, greeting{ id, seq, role, text, audio_url }, known_facts, open_questions }`
- `GET /api/v1/sessions/{id}` — `{ session, turns[] }`
- `POST /api/v1/sessions/{id}/turns` — `multipart/form-data`, поле `audio` (или `file`)  
  Ответ: `{ recognized_text, reply{ text, audio_url, ... }, user_turn, status }`
- `POST /api/v1/sessions/{id}/complete` — завершает сессию и запускает постобработку (sync/async по `PROCESSING_SYNC`)
- `GET /api/v1/sessions/{id}/draft` — черновик: рассказ, предложенные факты/люди/связи/вопросы (`items[]`)

### Подтверждение (только после явного решения рассказчика)
- `POST /api/v1/drafts/{session_id}/confirm`  
  ```json
  {
    "items": [
      { "item_id": "uuid", "action": "confirm|dismiss", "payload": { "...правки..." } }
    ],
    "story_text": "отредактированный рассказ (опц.)",
    "story_person_ids": ["uuid", "..."]
  }
  ```
  Подтверждённые пункты применяются атомарно в транзакции; неподтверждённые/отклонённые не меняют данные.
- `POST /api/v1/stories/{id}/confirm` — подтвердить/отредактировать рассказ.

### Рассказы / Медиа
- `GET /api/v1/stories/{id}`
- `GET /api/v1/media/{id}` — метаданные + актуальный URL
- `GET /api/v1/media/{id}/content` — проксирование байтов
- `POST /api/v1/media/upload` — универсальный multipart upload
- `POST /api/v1/media/presign` — presigned PUT (альтернатива)

### Вопросы (реестр)
- `GET /api/v1/questions?person_id=&status=open|resolved|dismissed&source=`
- `POST /api/v1/questions` — `{ text, person_id?, reason?, priority?, ... }`
- `PATCH /api/v1/questions/{id}` — `{ text?, status?, priority?, reason? }`

### Q&A
- `POST /api/v1/qa/ask` — `{ "question": "...", "scope_person_id": "uuid?" }`  
  Ответ: `{ "answer", "sources": [{person_id, person_name, story_id, fact_id, excerpt}], "insufficient_data": bool, "suggested_questions": [] }`

## Примеры запросов (curl)

```bash
BASE=http://localhost:8080/api/v1

# 1) Логин
TOKEN=$(curl -s -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"family","password":"change-me"}' | jq -r .token)

# 2) Создать человека
PERSON=$(curl -s -X POST $BASE/persons \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"display_name":"Иван Петров","birth_date":"1935","birth_approx":true,"places":["Воронеж"]}')
PID=$(echo "$PERSON" | jq -r .id)

# 3) Начать сессию (новый рассказ)
SESS=$(curl -s -X POST $BASE/sessions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"mode":"new_story"}')
SID=$(echo "$SESS" | jq -r .session.id)

# 4) Отправить реплику (аудио)
curl -s -X POST $BASE/sessions/$SID/turns \
  -H "Authorization: Bearer $TOKEN" \
  -F "audio=@sample.ogg" | jq .

# 5) Завершить и получить черновик
curl -s -X POST $BASE/sessions/$SID/complete -H "Authorization: Bearer $TOKEN"
curl -s $BASE/sessions/$SID/draft -H "Authorization: Bearer $TOKEN" | jq .

# 6) Подтвердить черновик (применяются только подтверждённые items)
curl -s -X POST $BASE/drafts/$SID/confirm \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"items":[{"item_id":"<draft_item_uuid>","action":"confirm"}],"story_text":"..."}' | jq .

# 7) Q&A по подтверждённым данным
curl -s -X POST $BASE/qa/ask \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"question":"Где родился Иван Петров?","scope_person_id":"'"$PID"'"}' | jq .
```

## Модель данных (ключевые таблицы)

`users`, `persons` (+ фото, варианты ФИО, approx-даты), `relationships`, `sessions`, `turns`, `transcripts`, `stories` + `story_persons`, `facts` (с `confirmed`), `questions` (реестр), `media`, `drafts` + `draft_items` (предложения ИИ до подтверждения), `jobs` (outbox).  
См. `migrations/000001_init.up.sql`.

## Интеграции Hugging Face и заглушки

- Интерфейсы: `ASR.Transcribe`, `LLM.Chat`, `TTS.Synthesize` (`internal/ai/interfaces.go`).
- HF-клиент: `internal/ai/hf` (Inference/Router HTTP; токен/модели/таймаут из env).
- Заглушки: `internal/ai/stub` — детерминированные ответы, пустая экстракция без выдумок, валидный WAV для TTS; результаты помечены `engine=fallback/stub`.
- `AI_FALLBACK_TO_STUB=true` (по умолчанию): при недоступности HF используется заглушка, ответ помечается `fallback=true` — клиент видит деградацию, но UX не ломается.

## Постобработка и подтверждение

1. `POST /sessions/{id}/complete` → задача `postprocess` (воркер/jobs или синхронно).
2. LLM-экстрактор возвращает JSON (люди/связи/факты/вопросы/рассказ); сохраняется только в `drafts`/`draft_items`.
3. Клиент показывает черновик и отправляет решения (`confirm|dismiss` с возможными правками).
4. Сервер в транзакции применяет **только подтверждённое**: создаёт/связывает сущности, фиксирует подтверждённый рассказ и факты. Неподтверждённое не влияет на дерево.

## Нефункциональные заметки (MVP)

- HTTPS-ready (рекомендуется запускать за reverse-proxy/TLS termination).
- Чувствительный контент (тексты/аудио) не логируется целиком; в логах — идентификаторы, статусы, длительности.
- Загрузки ограничены по размеру (аудио/фото) и привязаны к авторизованному пользователю.
- Идемпотентность: дубли вопросов в реестре подавляются уникальным индексом `(person_id, lower(text))`; повторное подтверждение обработанного черновика возвращает конфликт.
- Q&A отвечает **только** по подтверждённым записям и возвращает источники; при нехватке — `insufficient_data=true`.

## Ограничения MVP / дальнейшая работа

- Нет стриминга аудио и VAD; turn — целиком загруженный файл.
- Таймкоды транскрипта опциональны и зависят от ASR-модели.
- Поиск контекста для Q&A — простой (LIKE/последние факты/рассказы); для роста объёма — полнотекстовый поиск/векторный индекс.
- Очередь jobs — в PostgreSQL (достаточно для single-user); при масштабировании — вынести в брокер.
- Нет версионирования/конфликт-резолва карточек между несколькими клиентами (single-user допущение).
- Рекомендуется добавить rate limiting, аудит-лог подтверждений и резервное копирование БД/бакета.

## Сборка и проверка

```bash
go test ./...
go build ./cmd/api
```
