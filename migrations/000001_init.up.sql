-- Memory Button — начальная схема (MVP).
-- Все временные метки в TIMESTAMPTZ (UTC). Идентификаторы — UUID v4 (gen_random_uuid()).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------- Аккаунт (single-user) ----------
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- Медиа ----------
CREATE TABLE media (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        TEXT NOT NULL CHECK (kind IN ('audio', 'photo', 'other')),
    storage_key TEXT NOT NULL,
    url         TEXT NOT NULL DEFAULT '',
    mime        TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    checksum    TEXT NOT NULL DEFAULT '',
    owner_kind  TEXT NOT NULL DEFAULT 'session',
    owner_id    UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_media_owner ON media (owner_kind, owner_id);

-- ---------- Люди ----------
CREATE TABLE persons (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name   TEXT NOT NULL,
    name_variants  TEXT[] NOT NULL DEFAULT '{}',
    birth_date     TEXT,
    birth_approx   BOOLEAN NOT NULL DEFAULT FALSE,
    death_date     TEXT,
    death_approx   BOOLEAN NOT NULL DEFAULT FALSE,
    gender         TEXT CHECK (gender IN ('male', 'female', 'other') OR gender IS NULL),
    places         TEXT[] NOT NULL DEFAULT '{}',
    notes          TEXT NOT NULL DEFAULT '',
    photo_media_id UUID REFERENCES media (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_persons_display_name ON persons (display_name);

-- ---------- Связи ----------
CREATE TABLE relationships (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_person_id UUID NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    to_person_id   UUID NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    type          TEXT NOT NULL CHECK (type IN ('parent_child', 'spouse', 'sibling', 'other')),
    confidence    TEXT NOT NULL DEFAULT 'medium' CHECK (confidence IN ('high', 'medium', 'low')),
    source        TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('user', 'ai_draft', 'import')),
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_person_id <> to_person_id)
);
CREATE INDEX idx_relationships_from ON relationships (from_person_id);
CREATE INDEX idx_relationships_to ON relationships (to_person_id);

-- ---------- Сессии диалога ----------
CREATE TABLE sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode            TEXT NOT NULL CHECK (mode IN ('new_story', 'continue_person', 'clarify_person', 'qa')),
    person_id       UUID REFERENCES persons (id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'archived')),
    context_summary TEXT NOT NULL DEFAULT '',
    meta            JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_person ON sessions (person_id);
CREATE INDEX idx_sessions_status ON sessions (status);

-- ---------- Реплики (turn-by-turn) ----------
CREATE TABLE turns (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id     UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    seq            INTEGER NOT NULL,
    role           TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    text           TEXT NOT NULL DEFAULT '',
    audio_media_id UUID REFERENCES media (id) ON DELETE SET NULL,
    meta           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, seq)
);
CREATE INDEX idx_turns_session_seq ON turns (session_id, seq);

-- ---------- Транскрипты ----------
CREATE TABLE transcripts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    segments   JSONB NOT NULL DEFAULT '[]'::jsonb,
    engine     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_transcripts_session ON transcripts (session_id);

-- ---------- Рассказы ----------
CREATE TABLE stories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES sessions (id) ON DELETE SET NULL,
    text       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_stories_session ON stories (session_id);
CREATE INDEX idx_stories_status ON stories (status);

CREATE TABLE story_persons (
    story_id  UUID NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    person_id UUID NOT NULL REFERENCES persons (id) ON DELETE CASCADE,
    PRIMARY KEY (story_id, person_id)
);
CREATE INDEX idx_story_persons_person ON story_persons (person_id);

-- ---------- Факты ----------
CREATE TABLE facts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id      UUID REFERENCES persons (id) ON DELETE CASCADE,
    story_id       UUID REFERENCES stories (id) ON DELETE SET NULL,
    session_id     UUID REFERENCES sessions (id) ON DELETE SET NULL,
    type           TEXT NOT NULL,
    value          TEXT NOT NULL,
    normalized     JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence     NUMERIC(4, 3) NOT NULL DEFAULT 0.5,
    source_ref     TEXT NOT NULL DEFAULT '',
    source_excerpt TEXT NOT NULL DEFAULT '',
    confirmed      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_facts_person ON facts (person_id) WHERE confirmed;
CREATE INDEX idx_facts_story ON facts (story_id);
CREATE INDEX idx_facts_type ON facts (type);

-- ---------- Реестр вопросов ----------
CREATE TABLE questions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id         UUID REFERENCES persons (id) ON DELETE CASCADE,
    text              TEXT NOT NULL,
    source            TEXT NOT NULL DEFAULT 'dialogue'
                      CHECK (source IN ('dialogue', 'qa', 'extraction', 'manual')),
    reason            TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    priority          INTEGER NOT NULL DEFAULT 0,
    related_fact_id   UUID REFERENCES facts (id) ON DELETE SET NULL,
    related_session_id UUID REFERENCES sessions (id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ
);
CREATE INDEX idx_questions_person ON questions (person_id);
CREATE INDEX idx_questions_status ON questions (status);
-- Не дублируем один и тот же вопрос в рамках одного человека (person_id NULL = общий вопрос).
CREATE UNIQUE INDEX idx_questions_unique
    ON questions (COALESCE(person_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(text));

-- ---------- Черновики постобработки ----------
CREATE TABLE drafts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id   UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'dismissed')),
    story_text   TEXT NOT NULL DEFAULT '',
    meta         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    confirmed_at TIMESTAMPTZ
);
CREATE INDEX idx_drafts_session ON drafts (session_id);
CREATE INDEX idx_drafts_status ON drafts (status);

-- Предложенные ИИ изменения. В реальные таблицы попадают только после confirm.
CREATE TABLE draft_items (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    draft_id      UUID NOT NULL REFERENCES drafts (id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('person', 'relationship', 'fact', 'question', 'media_link', 'story')),
    action        TEXT NOT NULL DEFAULT 'create' CHECK (action IN ('create', 'update', 'link')),
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'dismissed')),
    resolved_ref_id UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_draft_items_draft ON draft_items (draft_id);

-- ---------- Очередь фоновых задач (outbox) ----------
CREATE TABLE jobs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_jobs_pending ON jobs (status, created_at);

-- updated_at триггеры
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'persons', 'relationships', 'sessions', 'stories', 'facts',
        'questions', 'drafts', 'draft_items', 'jobs'
    ] LOOP
        EXECUTE format('CREATE TRIGGER trg_%s_updated_at BEFORE UPDATE ON %s
                        FOR EACH ROW EXECUTE FUNCTION set_updated_at()', t, t);
    END LOOP;
END $$;
