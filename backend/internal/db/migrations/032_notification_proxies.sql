-- Прокси для исходящих запросов к API мессенджера: действует на все каналы провайдера
-- (свои и системные боты). URL шифруется — в нём может быть пароль прокси.

CREATE TABLE notification_proxies (
    provider      TEXT PRIMARY KEY CHECK (provider IN ('telegram', 'max')),
    url_encrypted BYTEA NOT NULL,
    updated_by    TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
