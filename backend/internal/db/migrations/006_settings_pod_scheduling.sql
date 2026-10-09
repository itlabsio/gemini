-- Планирование dump/restore-подов, редактируемое из UI (админка): nodeSelector /
-- tolerations / affinity. Формат — как в PodSpec k8s. Перекрывает значения из
-- values Helm-чарта (job.nodeSelector/…). Пусто ('{}') → берутся значения чарта.

ALTER TABLE app_settings
    ADD COLUMN default_pod_scheduling JSONB NOT NULL DEFAULT '{}'::jsonb;
