-- Базы, которые discovery инстанса должен игнорировать (служебные, шумные).
-- Помимо этого списка discovery всегда пропускает postgres: в Yandex MDB она
-- недоступна через пул (odyssey: route not found), а бэкап её не нужен нигде.

ALTER TABLE instances ADD COLUMN excluded_databases TEXT[] NOT NULL DEFAULT '{}';
