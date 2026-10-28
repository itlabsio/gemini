-- RBAC по проектам: проект — имя первого сегмента группы Keycloak
-- ("/<project>/<role>"). Проекты создаёт вручную superuser.
--
-- instances.project — NULL для уже существующих инстансов: управлять ими может
-- только superuser, пока не назначит проект. ON DELETE RESTRICT — проект с
-- инстансами удалить нельзя; ON UPDATE CASCADE — на случай переименования.

CREATE TABLE projects (
    name       TEXT PRIMARY KEY CHECK (name <> ''),
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE instances
    ADD COLUMN project TEXT REFERENCES projects(name) ON DELETE RESTRICT ON UPDATE CASCADE;

CREATE INDEX instances_project_idx ON instances(project);
