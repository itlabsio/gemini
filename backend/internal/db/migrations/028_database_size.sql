-- Примерный размер базы (pg_database_size) на момент последнего discovery.
-- NULL — discovery размер не прочитал (нет прав / ошибка) или ещё не запускался.
-- Только для информации в UI: на расписание, storage Job'ов и restore не влияет.

ALTER TABLE databases
    ADD COLUMN size_bytes BIGINT,
    ADD COLUMN size_updated_at TIMESTAMPTZ;
