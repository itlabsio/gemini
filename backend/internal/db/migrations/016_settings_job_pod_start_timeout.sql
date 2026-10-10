-- Сколько минут dump/restore-Job может ждать старта пода (поды не создаются —
-- FailedCreate, или висят в Pending). По истечении watcher удаляет Job и
-- закрывает прогон как failed — иначе он висел бы до activeDeadlineSeconds (6 ч).

ALTER TABLE app_settings
    ADD COLUMN job_pod_start_timeout_minutes INTEGER NOT NULL DEFAULT 10
        CHECK (job_pod_start_timeout_minutes BETWEEN 1 AND 1440);
