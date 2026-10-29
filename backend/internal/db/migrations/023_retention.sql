-- Сроки хранения журнала аудита и истории прогонов (дни), редактируемые из UI.
-- Чистит фоновый цикл на лидере (operator.RunRetention). Минимум 14 дней.
-- Из backup_runs не удаляются активные прогоны и последний прогон каждого вида
-- по базе: на event_key держится идемпотентность webhook'а и поллера.

ALTER TABLE app_settings
    ADD COLUMN audit_retention_days INTEGER NOT NULL DEFAULT 365
        CHECK (audit_retention_days BETWEEN 14 AND 3650),
    ADD COLUMN runs_retention_days  INTEGER NOT NULL DEFAULT 90
        CHECK (runs_retention_days BETWEEN 14 AND 3650);

-- Очистка идёт по created_at; для backup_runs есть только индекс (database_id, created_at).
CREATE INDEX backup_runs_created_idx ON backup_runs(created_at);
