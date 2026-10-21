-- Digest <key>.extensions, который Source подписал вместе с дампом (подпись v2):
-- "sha256:<hex>" | "none" (sidecar'а нет). '' — ещё не вычислен (прогоны до
-- миграции): BackfillSignatures досчитает его из бакета один раз и сохранит,
-- дальше проверка подписи обходится без повторного чтения sidecar'а.

ALTER TABLE backup_runs
    ADD COLUMN extensions_digest TEXT NOT NULL DEFAULT '';
