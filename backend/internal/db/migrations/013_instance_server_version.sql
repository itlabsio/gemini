-- Версия PostgreSQL-сервера инстанса, прочитанная последним discovery
-- (server_version_num, напр. 170004 = PG 17.4). 0 → discovery ещё не выполнялся.
-- Показывается в UI и подсказывает мажор pg_dump/pg_restore для Job'ов.
ALTER TABLE instances ADD COLUMN server_version_num INTEGER NOT NULL DEFAULT 0;
