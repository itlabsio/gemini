# Gemini — карта проекта

Бэкап/восстановление PostgreSQL между контурами. Один бинарь `gemini` играет роль
`HEAD_ROLE=source` (дампы) или `target` (restore). Общее описание — [README.md](README.md);
здесь только навигация «что где искать». Привязка к символам, а не к строкам.

Термины: говорим «инстанс Gemini», не «голова»; target — не обязательно DR.

## Куда идти по задаче

| Задача | Файлы |
|---|---|
| Новый/изменённый HTTP-маршрут | `backend/internal/api/server.go` (`NewRouter`, роли через `auth.RequireRole`), хендлер в соседнем файле `api/*.go` |
| Поле инстанса PostgreSQL / базы / сопоставления | миграция `db/migrations/NNN_*.sql` + `model/model.go` + `store/{instances,databases,mappings}.go` + `api/*` + `frontend/lib/types.ts` |
| Запуск dump/restore, статусы прогонов | `operator/operator.go`: `StartDump`, `StartRestore`, `Watch`, `reconcileOnce`, `onRunSucceeded`, `onRunFailed`, `CancelRun` |
| Расписание (CronJob'ы), шаблоны | `operator/scheduler.go` (`reconcileTemplates`), `operator.go:ensureCronJob`, `k8s/cronjobs.go` |
| Fallback-поллинг S3 на target | `operator/poller.go` (`pollOnce` — берёт только **последний** дамп префикса) |
| Сроки хранения аудита и прогонов | `operator/retention.go` (`RunRetention`), `store/retention.go` (`PruneRuns` не трогает активные и последний прогон вида по базе) |
| Подпись и проверка дампов (ed25519) | `operator.go`: `signDump`, `BackfillSignatures`, `VerifiedDump`; примитивы — `peer/peer.go` (`SignArtifact`, `VerifyArtifact`, v1/v2) |
| Webhook между инстансами Gemini | входящие: `api/webhook.go` (`backup-ready`, `restore-status`); исходящие: `operator.go` `notifyPeerBackupReady` / `notifyPeerRestoreStatus`; HMAC и retry — `peer/peer.go` |
| Pairing | `api/pairing.go` (код → exchange), `store/pairing.go`, `store/identity.go`, UI `frontend/app/pairing` |
| Что делает pod pg_dump / pg_restore | `jobrunner/dump.go` (`runDump`, `uploadDump`), `jobrunner/restore.go` (`restoreRecreate` / `restoreInPlace`, расширения, owner), выбор мажорной версии клиента — `jobrunner/pgclient.go` |
| Pod/Job: ресурсы, storage, overrides, таймауты | `k8s/podspec.go`, `k8s/podoverrides.go`, `k8s/jobs.go`; параметры из настроек — `operator.go:jobParams` |
| Секреты кред в k8s | `k8s/secrets.go`, `operator.go` (`resolveCreds`, `upsertInstanceCreds`, `upsertDatabaseCreds`), Vault — `vault/vault.go` |
| S3: несколько бакетов, fan-out | `storage/pool.go`, `storage/storage.go`, `api/s3buckets.go`, `store/s3buckets.go` |
| Discovery баз и ролей | `discovery/discovery.go`, `api/instances.go` (`discoverInstance`, `listInstanceRoles`) |
| Аутентификация/RBAC (client-роли superuser/security + роль в проекте из `groups`) | `auth/auth.go` (`clientRoles`, `Identity.Can`, `parseGroups`), `api/rbac.go` (`authorizeInstance/Database`, проекты), `api/me.go` |
| Шифрование секретов в БД метаданных | `secretcrypto/secretcrypto.go` (`SECRET_ENCRYPTION_KEY`) |
| Leader election, фоновые циклы | `k8s/leader.go`, `cmd/gemini/main.go` (`runServe`, `background`, `leaseName`) |
| Env-переменные backend'а | `config/config.go`, пример `backend/.env.local.example` |
| Уведомления (провалы/успехи) | `notify/notify.go`, фильтр по настройкам — `operator.go` `notifyFailure`/`notifySuccess` |
| Настройки (TTL, таймауты, scheduling, сроки хранения) | `api/settings.go`, `store/settings.go`, миграции 004/006/015/016/022/023 |
| Helm | `helm/gemini/{values.yaml,templates/}`, описание values — `helm/gemini/README.md`, примеры `helm/examples/` |
| CI/релиз | `.github/workflows/release.yml` (тег `vX.Y.Z`) |

## Backend (`backend/`, Go)

- `cmd/gemini/main.go` — подкоманды `serve | migrate | dump | restore`. `serve` миграции **не** применяет.
  `dump`/`restore` — это тот же бинарь внутри Job-пода (`jobrunner.Run`).
- `internal/api/` — маршруты: `/api/*` (OIDC JWT + роли), `/peer/*` и `/webhook/*` (HMAC от парного
  инстанса), `/pairing/exchange` (по коду), `/health/*`. Часть маршрутов регистрируется только для
  одной роли: `/pairing/issue-code` — source; `/pairing/init`, `/mapping*`, `/instances/{id}/roles` — target.
- `internal/operator/` — вся оркестрация. Фоновые циклы (watcher, template reconciler, poller,
  signature backfill) крутятся **только на лидере** (k8s Lease); API — на всех репликах.
- `internal/store/` — SQL поверх pgx, по файлу на сущность; `store.go` — пул и `mapErr`/`ErrNotFound`/`ErrConflict`.
- `internal/model/model.go` — все доменные типы и enum'ы (`BackupRun`, `Database`, `DatabaseMapping`, `HeadPairing`…).
- Тесты рядом с кодом (`*_test.go`): `cd backend && go test ./...`.

### Сущности и таблицы
`Instance` (сервер PostgreSQL) → `Database` (бэкапится/нет, cron) → `BackupRun` (kind dump|restore, trigger,
`event_key` для идемпотентности). Target: `DatabaseMapping` (source external_id ↔ локальная `Database`,
`s3_prefix`, `target_owner`). `HeadPairing`/`PairingSecret` — связь source↔target. `S3Bucket`, `Settings`, `Project` (RBAC; `instances.project`).
Миграции пронумерованы `001…024`; следующая — `025_*.sql`.

### Потоки
- **Backup (source):** CronJob/ручной запуск → `StartDump` → Job → `runDump` → S3 (`.dump`, `.sha256`, extensions)
  → `onRunSucceeded` → `signDump` → `notifyPeerBackupReady` (нет спаренных target'ов — просто пропуск).
- **Restore (target):** webhook `backup-ready` (`api/webhook.go`) **или** `pollOnce` → `VerifiedDump`
  (ed25519 против pinned-ключа source) → `StartRestore` → Job → `runRestore` → `notifyPeerRestoreStatus`.
- Префикс дампа в S3: `<имя инстанса>/<имя базы>/…` (`operator.resolveDumpKey`, `api/mapping.go:peerListDatabases`).

## Frontend (`frontend/`, Next.js 16 + next-auth, bun)

- **Перед правкой читать `frontend/AGENTS.md`** (Next.js здесь с breaking changes, доки — в `node_modules/next/dist/docs/`).
- `app/<раздел>/page.tsx` — страницы (`instances`, `mapping`, `pairing`, `runs`, `settings`), `actions.ts`
  рядом — server actions. `app/api/auth/[...nextauth]` — вход через Keycloak.
- `lib/api.ts` — клиент к backend, `lib/types.ts` — типы (держать в синхроне с `model.go`), `lib/auth.ts`, `proxy.ts`.
- `components/` — прикладные компоненты (`mapping-editor`, `databases-manager`, `instance-form`, `s3-buckets-manager`…),
  `components/ui/` — примитивы (shadcn-стиль).
- Менеджер пакетов — `bun` (`bun install`, `bun run dev`, `bun run lint`), не npm/npx.

## Helm (`helm/gemini/`)
Один релиз = один инстанс Gemini (`headRole: source|target`). Шаблоны разнесены на `templates/backend`
(deployment, migrate-job как pre-install/upgrade hook, service, configmap) и `templates/frontend`;
плюс `rbac.yaml` (права на Job/CronJob/Secret/Lease), `ingress.yaml`, `networkpolicy.yaml`.
БД метаданных, S3-бакет и Secret'ы чарт не создаёт.

## Правила работы в этом репо
- `kubectl` / `yc` локально не запускать без явного согласия пользователя (см. глобальный CLAUDE.md).
- Коммиты: короткий conventional subject без тела; автор — `ksrt12group@gmail.com`.
