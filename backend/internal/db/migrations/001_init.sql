-- Схема БД метаданных СРК Gemini (раздел 3 плана).
-- Применяется идентично на обеих головах; наполнение у каждой головы своё.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION gemini_touch_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Instance — сервер БД (source или target)
-- ---------------------------------------------------------------------------
CREATE TABLE instances (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role                     TEXT NOT NULL CHECK (role IN ('source', 'target')),
    name                     TEXT NOT NULL,
    host                     TEXT NOT NULL,
    port                     INT  NOT NULL DEFAULT 5432,
    auth_type                TEXT NOT NULL CHECK (auth_type IN ('plain', 'vault')),

    -- auth_type = plain: пароль лежит зашифрованным (AES-256-GCM на стороне приложения)
    plain_username           TEXT NOT NULL DEFAULT '',
    plain_password_encrypted BYTEA,

    -- auth_type = vault
    vault_path               TEXT NOT NULL DEFAULT '',
    vault_role               TEXT NOT NULL DEFAULT '',

    ssl_mode                 TEXT NOT NULL DEFAULT 'require',

    created_by               TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT instances_name_uniq UNIQUE (name)
);

CREATE TRIGGER instances_touch BEFORE UPDATE ON instances
    FOR EACH ROW EXECUTE FUNCTION gemini_touch_updated_at();

-- ---------------------------------------------------------------------------
-- Database — база на Instance, обнаруженная через discovery
-- ---------------------------------------------------------------------------
CREATE TABLE databases (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id   UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    db_name       TEXT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    schedule_cron TEXT NOT NULL DEFAULT '',

    -- Стабильный внешний идентификатор для межголовых вызовов.
    external_id   TEXT NOT NULL DEFAULT gen_random_uuid()::text,

    -- Присутствует ли база в последнем discovery.
    present       BOOLEAN NOT NULL DEFAULT TRUE,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT databases_instance_db_uniq UNIQUE (instance_id, db_name),
    CONSTRAINT databases_external_id_uniq UNIQUE (external_id)
);

CREATE INDEX databases_instance_idx ON databases(instance_id);
CREATE INDEX databases_enabled_idx  ON databases(enabled) WHERE enabled;

CREATE TRIGGER databases_touch BEFORE UPDATE ON databases
    FOR EACH ROW EXECUTE FUNCTION gemini_touch_updated_at();

-- ---------------------------------------------------------------------------
-- HeadPairing — доверие между головами
-- ---------------------------------------------------------------------------
CREATE TABLE head_pairings (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    peer_role        TEXT NOT NULL CHECK (peer_role IN ('source', 'target')),
    peer_url         TEXT NOT NULL DEFAULT '',

    -- Одноразовый pairing-код (bcrypt/argon-подобный хэш) — только пока status='pending'.
    pairing_code_hash TEXT NOT NULL DEFAULT '',
    code_expires_at   TIMESTAMPTZ,

    -- Материал для HMAC-подписи runtime-запросов. Сам секрет по сети повторно не ходит.
    auth_secret_hash  TEXT NOT NULL DEFAULT '',

    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'revoked')),
    established_at    TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX head_pairings_single_active
    ON head_pairings(peer_role) WHERE status = 'active';

-- ---------------------------------------------------------------------------
-- DatabaseMapping — сопоставление source-базы и локальной target-базы (живёт на DR)
-- ---------------------------------------------------------------------------
CREATE TABLE database_mappings (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    head_pairing_id             UUID NOT NULL REFERENCES head_pairings(id) ON DELETE CASCADE,
    source_database_external_id TEXT NOT NULL,
    source_db_name              TEXT NOT NULL DEFAULT '',
    target_database_id          UUID NOT NULL REFERENCES databases(id) ON DELETE RESTRICT,
    s3_prefix                   TEXT NOT NULL DEFAULT '',
    enabled                     BOOLEAN NOT NULL DEFAULT TRUE,

    created_by                  TEXT NOT NULL DEFAULT '',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT database_mappings_source_uniq UNIQUE (head_pairing_id, source_database_external_id)
);

CREATE INDEX database_mappings_source_ext_idx ON database_mappings(source_database_external_id);

CREATE TRIGGER database_mappings_touch BEFORE UPDATE ON database_mappings
    FOR EACH ROW EXECUTE FUNCTION gemini_touch_updated_at();

-- ---------------------------------------------------------------------------
-- BackupRun — история прогонов dump / restore
-- ---------------------------------------------------------------------------
CREATE TABLE backup_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id   UUID NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('dump', 'restore')),
    trigger       TEXT NOT NULL CHECK (trigger IN ('scheduled', 'manual', 'webhook', 'poll-fallback')),
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    k8s_job_name  TEXT NOT NULL DEFAULT '',
    s3_object_key TEXT NOT NULL DEFAULT '',
    checksum      TEXT NOT NULL DEFAULT '',

    -- Бизнес-ключ события для идемпотентности webhook/poll-обработчиков.
    event_key     TEXT NOT NULL DEFAULT '',

    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT '',
    initiated_by  TEXT NOT NULL DEFAULT '',

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX backup_runs_database_idx ON backup_runs(database_id, created_at DESC);
CREATE INDEX backup_runs_status_idx   ON backup_runs(status);
CREATE UNIQUE INDEX backup_runs_event_key_uniq ON backup_runs(event_key) WHERE event_key <> '';

-- Не более одного активного прогона на (базу, вид) одновременно.
CREATE UNIQUE INDEX backup_runs_one_active
    ON backup_runs(database_id, kind)
    WHERE status IN ('pending', 'running');

-- ---------------------------------------------------------------------------
-- User — кэш логинов для аудита UI
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    subject      TEXT PRIMARY KEY,
    email        TEXT NOT NULL DEFAULT '',
    roles        TEXT[] NOT NULL DEFAULT '{}',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- audit_log — кто что создал/изменил/запустил (раздел 7, этап 13)
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor      TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    detail     JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX audit_log_created_idx ON audit_log(created_at DESC);
