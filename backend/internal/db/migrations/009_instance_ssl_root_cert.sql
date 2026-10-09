-- Кастомный корневой CA для self-hosted инстансов с собственным SSL-сертификатом.
--
-- Пусто → доверяемся системному trust store образа (там Yandex Cloud CA и
-- публичные CA) — это поведение по умолчанию для verify-ca/verify-full.
-- Непусто → PEM корневого сертификата, который голова прокидывает в discovery и
-- в dump/restore-Job'ы (PGSSLROOTCERT). Не секрет: живёт в instances рядом с
-- ssl_mode, а не в instance_credentials.

ALTER TABLE instances ADD COLUMN ssl_root_cert TEXT NOT NULL DEFAULT '';
