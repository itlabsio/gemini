-- Дополнительные S3-бакеты дампов, управляемые из UI.
--
-- Fan-out: Source заливает каждый дамп во ВСЕ включённые бакеты (свой из Helm
-- S3_* + эти), Target ищет дамп во всех своих и качает из первого, где он есть.
-- Ключ объекта одинаков во всех бакетах, поэтому подпись (.sig) и event_key от
-- бакета не зависят. Списки бакетов у голов независимые.

CREATE TABLE s3_buckets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    endpoint   TEXT NOT NULL,
    region     TEXT NOT NULL DEFAULT '',
    bucket     TEXT NOT NULL,

    access_key_id               TEXT  NOT NULL,
    -- AES-256-GCM на стороне приложения, как plain-пароли инстансов
    secret_access_key_encrypted BYTEA NOT NULL,

    path_style BOOLEAN NOT NULL DEFAULT TRUE,
    use_ssl    BOOLEAN NOT NULL DEFAULT TRUE,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,

    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT s3_buckets_name_uniq UNIQUE (name)
);

CREATE TRIGGER s3_buckets_touch BEFORE UPDATE ON s3_buckets
    FOR EACH ROW EXECUTE FUNCTION gemini_touch_updated_at();
