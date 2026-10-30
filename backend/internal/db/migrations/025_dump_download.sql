-- Скачивание дампов админами проекта по presigned-ссылке (только Source).
-- instances.downloadable — разрешено ли скачивание дампов баз инстанса; по
-- умолчанию выключено, включает только superuser (прод-данные).
-- s3_buckets.downloadable — из каких бакетов можно выдавать ссылки (бакет должен быть
-- доступен из браузера пользователя); у встроенного бакета — env S3_DOWNLOADABLE.
-- download_url_ttl_minutes — срок жизни ссылки; потолок — 7 дней (предел SigV4).

ALTER TABLE instances
    ADD COLUMN downloadable BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE s3_buckets
    ADD COLUMN downloadable BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE app_settings
    ADD COLUMN download_url_ttl_minutes INTEGER NOT NULL DEFAULT 15
        CHECK (download_url_ttl_minutes BETWEEN 1 AND 10080);
