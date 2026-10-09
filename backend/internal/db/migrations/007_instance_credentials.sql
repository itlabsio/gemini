-- Креды подключения переезжают из instances в отдельную таблицу.
--
-- Зачем: в instances кредовая часть — это tagged union (ветка plain / ветка
-- vault), где половина колонок всегда пустая, и растёт она при каждом новом
-- методе аутентификации, тогда как адресная часть (host/port/ssl_mode) стабильна.
-- Заодно инварианты вида «для vault обязателен vault_path» переезжают из Go в
-- CHECK, а секретный материал (plain_password_encrypted) изолируется в одной
-- таблице — её можно отдельно грантовать и аудировать.
--
-- Связь строго 1:1, поэтому instance_id — это и PK, и FK с каскадным удалением.

CREATE TABLE instance_credentials (
    instance_id UUID PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    auth_type   TEXT NOT NULL CHECK (auth_type IN ('plain', 'vault')),

    -- auth_type = plain: пароль зашифрован AES-256-GCM на стороне приложения
    plain_username           TEXT  NOT NULL DEFAULT '',
    plain_password_encrypted BYTEA,

    -- auth_type = vault. vault_path — полный путь Vault API (вместе с mount и,
    -- для KV v2, сегментом data/). vault_*_key — какие поля секрета считать
    -- логином и паролем; пусто → эвристика по известным именам.
    vault_path         TEXT NOT NULL DEFAULT '',
    vault_role         TEXT NOT NULL DEFAULT '',
    vault_username_key TEXT NOT NULL DEFAULT '',
    vault_password_key TEXT NOT NULL DEFAULT '',

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO instance_credentials
    (instance_id, auth_type, plain_username, plain_password_encrypted, vault_path, vault_role)
SELECT id, auth_type, plain_username, plain_password_encrypted, vault_path, vault_role
  FROM instances;

-- Констрейнты добавляем после переноса: если в старых данных есть неполная
-- запись, ошибка укажет прямо на неё, а не на INSERT целиком.
ALTER TABLE instance_credentials
    ADD CONSTRAINT creds_plain_complete CHECK (auth_type <> 'plain' OR plain_username <> ''),
    ADD CONSTRAINT creds_vault_complete CHECK (auth_type <> 'vault' OR vault_path <> '');

CREATE TRIGGER instance_credentials_touch BEFORE UPDATE ON instance_credentials
    FOR EACH ROW EXECUTE FUNCTION gemini_touch_updated_at();

ALTER TABLE instances
    DROP COLUMN auth_type,
    DROP COLUMN plain_username,
    DROP COLUMN plain_password_encrypted,
    DROP COLUMN vault_path,
    DROP COLUMN vault_role;
