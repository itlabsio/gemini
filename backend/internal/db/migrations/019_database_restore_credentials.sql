-- Режим restore in_place_per_db: накат как in_place, но pg_restore подключается
-- не пользователем инстанса, а своим пользователем на каждую target-базу.
--
-- Зачем: в managed-кластерах без суперюзера (Selectel) роль-владельца базы
-- нельзя выдать restorer'у — у роли нет ADMIN OPTION ни у кого, кроме системного
-- суперюзера, и `pg_restore --role=<owner>` падает "permission denied to set
-- role". Подключаясь самим владельцем (или его членом), SET ROLE не нужен.
--
-- Обрыв сессий и CREATE EXTENSION по-прежнему идут под пользователем инстанса:
-- ему нужны права на чужие сессии, владельцу базы — нет.

ALTER TABLE instances DROP CONSTRAINT instances_restore_mode_check;
ALTER TABLE instances ADD CONSTRAINT instances_restore_mode_check
	CHECK (restore_mode IN ('recreate', 'in_place', 'in_place_per_db'));

-- Креды restore базы: 1:1 с databases. Пароль зашифрован AES-256-GCM на стороне
-- приложения (как в instance_credentials) и наружу не отдаётся.
CREATE TABLE database_credentials (
    database_id        UUID PRIMARY KEY REFERENCES databases(id) ON DELETE CASCADE,
    username           TEXT  NOT NULL CHECK (username <> ''),
    password_encrypted BYTEA NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER database_credentials_touch BEFORE UPDATE ON database_credentials
    FOR EACH ROW EXECUTE FUNCTION gemini_touch_updated_at();
