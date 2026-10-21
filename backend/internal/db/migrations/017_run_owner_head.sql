-- Голова-владелец прогона: UID Deployment'а backend'а, который его создал.
-- Reaper закрывает «прогон без Job'а» только у своих прогонов: голова, по ошибке
-- подключённая к той же БД из другого кластера/namespace, своих Job'ов там не
-- видит и иначе гасила бы чужие прогоны через stuckRunGrace. '' — прогоны,
-- созданные до этой миграции (или без резолва Deployment'а).

ALTER TABLE backup_runs
    ADD COLUMN owner_head TEXT NOT NULL DEFAULT '';
