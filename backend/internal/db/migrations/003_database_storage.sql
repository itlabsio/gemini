-- Временное хранение дампа настраивается на каждую базу: эфемерная PVC
-- (generic ephemeral volume) или emptyDir. Пустые значения = дефолты головы.

ALTER TABLE databases ADD COLUMN storage_type TEXT NOT NULL DEFAULT ''
    CHECK (storage_type IN ('', 'ephemeral', 'emptydir'));
ALTER TABLE databases ADD COLUMN storage_size TEXT NOT NULL DEFAULT '';
