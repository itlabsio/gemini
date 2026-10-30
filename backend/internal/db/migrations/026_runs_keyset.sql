-- Пагинация истории прогонов курсором (created_at, id): id — UUID, порядка не
-- даёт, поэтому ключ составной. Индекс заменяет backup_runs_created_idx
-- (очистка по created_at пользуется им же).

CREATE INDEX backup_runs_created_id_idx ON backup_runs(created_at, id);
DROP INDEX backup_runs_created_idx;
