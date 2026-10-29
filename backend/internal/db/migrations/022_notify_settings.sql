-- На какие события слать уведомления (Slack / Telegram, настраиваются в чарте).
-- По умолчанию — только провалы, как было до появления настройки.

ALTER TABLE app_settings
    ADD COLUMN notify_on_failure BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN notify_on_success BOOLEAN NOT NULL DEFAULT FALSE;
