-- Восстановление на Target.
-- instances.restorable — предохранитель инстанса: FALSE запрещает запуск любого
-- restore в его базы (и автоматического, и ручного); идущие прогоны доживают.
-- Меняет только superuser.
-- databases.enabled на Target теперь означает только автовосстановление
-- (webhook backup-ready и fallback-поллинг); ручной restore от него не зависит.
-- database_mappings.enabled убран: UI его не выставлял, а «пауза» связи
-- дублировала флаг базы.

ALTER TABLE instances
    ADD COLUMN restorable BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE database_mappings
    DROP COLUMN enabled;
