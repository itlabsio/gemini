-- Глобальные дефолты СРК, редактируемые из UI (админка): тип и размер временного
-- хранилища дампов, ресурсы dump/restore-подов. Одна строка (singleton).

CREATE TABLE app_settings (
    id                   BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    default_storage_type TEXT NOT NULL DEFAULT 'ephemeral'
        CHECK (default_storage_type IN ('ephemeral', 'emptydir')),
    default_storage_size TEXT NOT NULL DEFAULT '20Gi',
    -- {"requests":{"cpu":"500m","memory":"512Mi"},"limits":{"cpu":"2","memory":"2Gi"}}
    default_resources    JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by           TEXT NOT NULL DEFAULT ''
);

INSERT INTO app_settings (id) VALUES (TRUE) ON CONFLICT DO NOTHING;
