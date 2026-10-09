-- TTL завершённых dump/restore-Job'ов в минутах (→ spec.ttlSecondsAfterFinished),
-- редактируемый из UI (админка). Через столько минут после завершения k8s удаляет
-- Job и его поды. Минимум 5 минут — watcher должен успеть увидеть статус Job'а.

ALTER TABLE app_settings
    ADD COLUMN job_ttl_minutes INTEGER NOT NULL DEFAULT 5
        CHECK (job_ttl_minutes BETWEEN 5 AND 10080);
