-- Роль-владелец восстановленной базы на target-кластере.
--
-- Дампы снимаются с --no-owner/--no-privileges, restore идёт под суперюзером
-- (пользователь target-инстанса). Без явного владельца восстановленная база и
-- все её объекты принадлежали бы суперюзеру. target_owner — заранее созданная
-- роль на target-кластере; restore-Job делает CREATE DATABASE ... OWNER и
-- SET SESSION AUTHORIZATION под неё, а в конце REASSIGN OWNED на всякий случай.
--
-- Пусто → поведение как раньше (владелец = restorer). Новые сопоставления через
-- API сохранить без владельца нельзя.

ALTER TABLE database_mappings ADD COLUMN target_owner TEXT NOT NULL DEFAULT '';
