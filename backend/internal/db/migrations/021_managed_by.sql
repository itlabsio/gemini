-- managed_by — кто управляет ресурсом вне UI. 'terraform' ставит backend, когда
-- запрос пришёл от terraform-provider-gemini (заголовок X-Gemini-Managed-By).
-- UI показывает бейдж: правки через UI перезапишет следующий terraform apply.
-- Снимает метку superuser (ресурс убрали из Terraform).

ALTER TABLE instances
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT '' CHECK (managed_by IN ('', 'terraform'));

ALTER TABLE projects
    ADD COLUMN managed_by TEXT NOT NULL DEFAULT '' CHECK (managed_by IN ('', 'terraform'));
