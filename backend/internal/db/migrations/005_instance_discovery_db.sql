-- База, к которой discovery подключается для чтения pg_database.
-- Пусто = "postgres". В Yandex Managed PostgreSQL база postgres недоступна
-- через пул (odyssey: route not found) — нужно указать существующую
-- пользовательскую базу.

ALTER TABLE instances ADD COLUMN discovery_db TEXT NOT NULL DEFAULT '';
