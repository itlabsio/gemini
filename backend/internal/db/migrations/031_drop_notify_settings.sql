-- Какие исходы слать, теперь задаётся у каждого канала (030). Колонки удаляются
-- отдельным релизом: при выкатке этой миграции работают поды 2.0.0, которые их уже не читают.

ALTER TABLE app_settings
    DROP COLUMN notify_on_failure,
    DROP COLUMN notify_on_success;
