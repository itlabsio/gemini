-- Каналы уведомлений настраиваются в Gemini, а не в чарте. Канал с project IS NULL —
-- глобальный (все события, управляет superuser), иначе — канал проекта.
-- bot_token_encrypted IS NULL — канал шлёт через системного бота своего провайдера.
--
-- app_settings.notify_on_* больше не используются, но удаляются отдельной миграцией
-- в следующем релизе: миграция идёт до выкатки, и старые поды читают эти колонки.

CREATE TABLE notification_bots (
    provider        TEXT PRIMARY KEY CHECK (provider IN ('telegram', 'max')),
    token_encrypted BYTEA NOT NULL,
    bot_name        TEXT NOT NULL,
    updated_by      TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE notification_channels (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project             TEXT REFERENCES projects(name) ON DELETE CASCADE ON UPDATE CASCADE,
    name                TEXT NOT NULL,
    provider            TEXT NOT NULL DEFAULT 'telegram' CHECK (provider IN ('telegram', 'max')),
    bot_token_encrypted BYTEA,
    bot_name            TEXT NOT NULL DEFAULT '',
    chat_id             TEXT NOT NULL,
    thread_id           TEXT NOT NULL DEFAULT '',
    on_failure          BOOLEAN NOT NULL DEFAULT TRUE,
    on_success          BOOLEAN NOT NULL DEFAULT FALSE,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    created_by          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX notification_channels_project_idx ON notification_channels (project);
