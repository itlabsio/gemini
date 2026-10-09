-- Стратегия restore для target-инстанса.
--
--   recreate  — ALTER DATABASE RENAME → CREATE DATABASE → накат → DROP old.
--               Держит прежнюю копию базы до успеха. Требует прав на RENAME и
--               CREATE DATABASE — self-hosted.
--   in_place  — накат дампа (снят с --clean --if-exists) прямо в существующую
--               базу. Для Yandex Managed PostgreSQL, где RENAME/CREATE DATABASE
--               недоступны. Прежней копии не остаётся.
--
-- Дефолт recreate сохраняет поведение существующих self-hosted инстансов.

ALTER TABLE instances
	ADD COLUMN restore_mode TEXT NOT NULL DEFAULT 'recreate'
	CHECK (restore_mode IN ('recreate', 'in_place'));
