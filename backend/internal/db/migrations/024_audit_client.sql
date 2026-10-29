-- Откуда пришёл запрос, попавший в журнал аудита: IP клиента (X-Real-IP от
-- ingress, иначе адрес соединения) и User-Agent. Плюс индексы под фильтры
-- страницы журнала (пользователь, действие) с сортировкой по времени.

ALTER TABLE audit_log
    ADD COLUMN ip         TEXT NOT NULL DEFAULT '',
    ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';

CREATE INDEX audit_log_actor_idx  ON audit_log(actor, id DESC);
CREATE INDEX audit_log_action_idx ON audit_log(action, id DESC);
