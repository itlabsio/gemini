# helm/gemini

Единый чарт для **одной головы** Gemini (backend + frontend + RBAC). Роль головы —
`values.headRole` (`source` | `dr`); `helm/examples/values-source.yaml` и
`helm/examples/values-dr.yaml` — два набора оверрайдов одного и того же чарта.

```bash
helm lint ./gemini -f examples/values-source.yaml
helm template gemini-source ./gemini -f examples/values-source.yaml --output-dir out

helm install gemini-source ./gemini -f examples/values-source.yaml -n gemini --create-namespace
helm install gemini-dr     ./gemini -f examples/values-dr.yaml     -n gemini --create-namespace
```

## Что деплоит

| Ресурс | Условие |
|---|---|
| Deployment/Service `*-backend` | всегда |
| Deployment/Service `*-frontend` | всегда |
| Job `*-migrate` (helm `pre-install,pre-upgrade` + Argo CD `PreSync`) | `migrate.enabled` |
| ConfigMap `*-backend`, `*-frontend` | всегда |
| ServiceAccount `gemini` + `gemini-job` | `rbac.create` |
| namespaced Role/RoleBinding (`cronjobs`,`jobs`,`secrets`,`pods`,`pods/log`,`deployments:get`) | `rbac.create` |
| ClusterRole/ClusterRoleBinding на `storageclasses` (read) | `rbac.create && rbac.storageClassRead` |
| Ingress (`/api/auth` → frontend; `/api`,`/peer`,`/webhook`,`/pairing/exchange` → backend; `/` → frontend; health-probes наружу не выставляются) | `ingress.enabled` |
| NetworkPolicy (egress backend: DNS + 443/6443 + 5432/6432, +8200 при `vault.enabled`) | `networkPolicy.enabled` |

## Чего чарт НЕ делает

- **не разворачивает свою БД метаданных** — ожидается внешний PostgreSQL, dsn через `ownDatabase.existingSecret` (`POSTGRES_URL`);
- **не создаёт бакет S3** и его object lock / lifecycle policy — это Terraform;
- **не создаёт Secret'ы** — все секреты передаются готовыми k8s Secret'ами
  (`*.existingSecret`). Обязательный минимум: dsn БД, `SECRET_ENCRYPTION_KEY` (32 байта),
  ключ S3, `KEYCLOAK_CLIENT_SECRET` + `AUTH_SECRET` для frontend.
