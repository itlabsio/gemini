-- Асимметричная подпись дампов + несколько Target на один Source.

-- Ключ головы: приватником Source подписываются дампы, Target'ы проверяют
-- запиненным публичным ключом. Одна строка на голову; генерится при старте.
CREATE TABLE head_identity (
    singleton   BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    private_key TEXT NOT NULL,  -- base64(nonce||ciphertext), AES-256-GCM
    public_key  TEXT NOT NULL,  -- base64 сырых 32 байт Ed25519
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Публичный ключ пира, полученный при pairing.
ALTER TABLE head_pairings ADD COLUMN peer_public_key TEXT NOT NULL DEFAULT '';

-- Source держит несколько active-пар с разными Target'ами. Уникальность —
-- по адресу пира среди active: повторный pairing того же Target заменяет,
-- новый Target добавляется.
DROP INDEX head_pairings_single_active;
CREATE UNIQUE INDEX head_pairings_active_peer_uniq
    ON head_pairings (peer_role, peer_url) WHERE status = 'active';

-- Исход restore по каждому Target отдельно (заменяет peer_* колонки backup_runs).
CREATE TABLE backup_run_peer_status (
    run_id      UUID NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    pairing_id  UUID NOT NULL REFERENCES head_pairings(id) ON DELETE CASCADE,
    peer_url    TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (run_id, pairing_id)
);

CREATE INDEX backup_run_peer_status_run_idx ON backup_run_peer_status(run_id);

ALTER TABLE backup_runs
    DROP COLUMN peer_status,
    DROP COLUMN peer_error,
    DROP COLUMN peer_reported_at;
