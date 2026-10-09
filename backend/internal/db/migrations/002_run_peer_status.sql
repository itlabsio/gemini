-- Обратный webhook restore-status от DR: Source сохраняет исход применения дампа
-- в DR-контуре рядом со своим dump-прогоном, чтобы админ видел статус, не заходя в DR.

ALTER TABLE backup_runs ADD COLUMN peer_status      TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_runs ADD COLUMN peer_error       TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_runs ADD COLUMN peer_reported_at TIMESTAMPTZ;

-- Быстрый поиск dump-прогона по ключу объекта при приёме обратного webhook.
CREATE INDEX backup_runs_s3_object_key_idx ON backup_runs(s3_object_key) WHERE s3_object_key <> '';
