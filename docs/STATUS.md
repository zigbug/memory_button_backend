# Статус реализации (MVP)

## Готово
- Структура Go-проекта, конфиг через env, логирование, healthcheck.
- PostgreSQL: миграции (auto), модели и репозитории (users, persons, relationships, sessions, turns, transcripts, stories/facts, questions, media, drafts/draft_items, jobs).
- S3-совместимое хранилище: MinIO и файловый режим (`STORAGE_DRIVER=fs`) + метаданные медиа.
- Авторизация single-user: bcrypt + JWT HS256, `/auth/login`, `/me`, опц. статический bearer.
- API `/api/v1`: persons/relationships/sessions(turns)/stories/media/questions/qa + подтверждение черновиков.
- Диалоговый оркестратор: режимы `new_story|continue_person|clarify_person|qa`, приветствие с контекстом, ASR→LLM→TTS turn, сводка сессии.
- Постобработка: экстракция в черновик (рассказ/люди/связи/факты/вопросы), outbox-воркер/синхронный режим.
- Подтверждение: атомарное применение только подтверждённых пунктов (`/drafts/{session_id}/confirm`), без молчаливых изменений дерева.
- Q&A: ответы только по подтверждённым данным с `sources[]`, `insufficient_data` и предложениями вопросов.
- AI: интерфейсы HF (ASR/LLM/TTS), stub и graceful fallback.
- Документация: README, `docs/API.md`, `docs/architecture.md`, `docs/openapi.yaml`, Swagger UI (`/docs/`).
- Dockerfile + docker-compose, деплой на VPS (nginx proxy).

## Известные ограничения / далее
- Нет стриминга/VAD и точных таймкодов (зависит от ASR); turn — целый аудиофайл.
- Поиск контекста Q&A упрощённый (LIKE/последние подтверждённые записи) — для роста нужен FTS/векторный индекс.
- Presigned upload реализован как API-возможность; основной поток — multipart через API.
- Нет rate limiting, расширенного аудита и UI администрирования; рекомендуется TLS, бэкапы БД/хранилища.
- OpenAPI описывает основной контракт MVP; при расширении API — синхронизировать спеку.
