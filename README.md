# Gemini

СРК (система резервного копирования) PostgreSQL: по расписанию снимает дампы
баз в исходном контуре, складывает их в S3 и восстанавливает в другом контуре —
резервном (DR), тестовом, стейджинге или любом другом, куда нужна свежая копия
данных.

Система состоит из **инстансов Gemini** — одинаковых инсталляций (backend + frontend),
которые различаются только ролью `HEAD_ROLE`. Не путать с инстансами PostgreSQL —
серверами БД, которые заводятся в UI как источники и приёмники дампов.

| Роль | Где живёт | Что делает |
|---|---|---|
| `source` | исходный контур | discovery баз на source-серверах PostgreSQL, CronJob'ы `pg_dump`, заливка дампов в S3, подпись дампов, webhook `backup-ready` спаренным target-инстансам |
| `target` | принимающий контур (DR, тест, стейджинг…) | сопоставление source-баз с локальными, `pg_restore` по webhook'у или по fallback-поллингу S3, обратный webhook `restore-status` |

К одному source-инстансу можно подключить несколько target'ов — каждый со своими
сопоставлениями баз; source показывает исход restore по каждому.

У каждого инстанса Gemini своя БД метаданных; через границу контуров ходят только
метаданные событий (id базы, ключ объекта в S3, статус) — секреты подключений
никогда.

## Как это работает

```
 исходный контур                                 принимающий контур
┌──────────────────────┐                        ┌──────────────────────┐
│ Gemini (source)      │  POST /webhook/        │ Gemini (target)      │
│  CronJob gemini dump ├─── backup-ready ──────►│  Job gemini restore  │
│        │             │◄── restore-status ─────┤        ▲             │
└────────┼─────────────┘    (HMAC-подпись)      └────────┼─────────────┘
         │ pg_dump -Fc + .sha256 + .sig                  │ pg_restore
         ▼                                               │
      ┌──────────────── S3 (один или несколько бакетов) ─┘
      └─ fan-out на запись, restore — из первого бакета, где есть дамп
```

1. **Pairing.** Admin на source выпускает одноразовый код (TTL 15 мин), admin
   на target вводит URL source и код. Инстансы обмениваются общим HMAC-секретом и
   пинят публичные Ed25519-ключи друг друга. Все запросы между инстансами
   (`/peer/*`, `/webhook/*`) подписываются HMAC с проверкой времени
   (`PEER_CLOCK_SKEW`).
2. **Серверы и базы.** В UI заводятся инстансы PostgreSQL (креды — plain,
   шифруются AES-256-GCM в БД метаданных, или путь в Vault). Discovery
   находит базы; для каждой задаётся расписание и временное хранилище.
3. **Dump (source).** Gemini держит CronJob на базу; под — тот же образ
   backend'а с подкомандой `gemini dump`. Креды попадают в под через
   ephemeral Secret. Дамп снимается `pg_dump` мажорной версии сервера
   (в образе клиенты 16/17/18), заливается во все включённые бакеты вместе с
   `.sha256`, `.extensions` и Ed25519-подписью `.sig`.
4. **Restore (target).** На `backup-ready` (или при fallback-поллинге S3 раз в
   `POLL_INTERVAL`) target проверяет префикс сопоставления, контрольную сумму и
   подпись и запускает Job `gemini restore`. Повторы отсекаются по `event_key`.
   Режим restore задаётся на target-инстансе PostgreSQL:
   - `recreate` — RENAME → CREATE → накат → DROP старой (self-hosted);
   - `in_place` — `pg_restore --clean --if-exists` в существующую базу (managed);
   - `in_place_per_db` — как `in_place`, но под пользователем каждой базы.
5. **Статусы.** Target сообщает исход restore обратно, source показывает его
   в истории прогонов. Опционально — уведомления в Slack/Telegram.

Фоновые циклы (watcher Job'ов, сверка CronJob'ов, поллер, до-подписание дампов)
работают только на лидере (k8s Lease), API обслуживают все реплики.

## Доступ

Аутентификация — OIDC (Keycloak). Frontend логинится через next-auth
(code + PKCE), backend проверяет JWT. Права складываются из client-ролей и групп:

- **superuser** — client-роль `superuser` в `resource_access.<OIDC_CLIENT_ID>.roles`.
  Управляет всем инстансом Gemini: проекты, создание инстансов и назначение им
  проекта, pairing, настройки, S3-бакеты. Видит все инстансы.
- **security** — client-роль `security` там же. Для ИБ: только чтение журнала
  аудита (страница «Аудит»), больше ничего. У source и target журналы свои —
  роль нужна в клиенте каждого инстанса Gemini.
- **роль в проекте** — из claim `groups` (в Keycloak включить **Full group path**):
  группа `/<проект>/<роль>`. Проект — первый сегмент пути, роль — одно из
  `admin`, `developer` (= operator), `readonly` (= viewer). Одному пользователю
  можно дать разные роли в разных проектах. Группы без распознанной роли
  игнорируются; при нескольких ролях в проекте берётся наивысшая.

| Роль в проекте | Права на инстансы проекта |
|---|---|
| `readonly` | просмотр инстанса, баз, прогонов |
| `developer` | + ручной запуск и отмена прогонов |
| `admin` | + правка/удаление инстанса, discovery, настройки баз, сопоставления |

Инстанс принадлежит проекту (`instances.project`); без проекта (например, после
миграции `020`) он виден и управляем только superuser, пока тот не назначит проект
на странице инстанса. Проекты создаёт superuser на вкладке «Проекты», их имена
должны совпадать с группами Keycloak и **на Source, и на Target**: сопоставление
баз идёт только между инстансами одноимённого проекта.

## Структура репозитория

```
backend/            Go: API инстанса Gemini и dump/restore-раннер (один бинарь)
  cmd/gemini/       точка входа: serve | migrate | dump | restore
  internal/
    api/            HTTP-маршруты (/api, /peer, /webhook, /pairing)
    operator/       оркестрация прогонов, scheduler, poller
    jobrunner/      код внутри dump/restore-пода (pg_dump / pg_restore)
    k8s/            Job/CronJob/Secret, pod overrides, leader election
    peer/           HTTP-клиент между инстансами Gemini, HMAC и подписи артефактов
    storage/        S3-пул (fan-out по бакетам)
    store/, db/     БД метаданных и SQL-миграции
    auth/, vault/, notify/, secretcrypto/, discovery/
frontend/           Next.js 16 + next-auth, UI инстанса Gemini (bun)
helm/gemini/        чарт одного инстанса Gemini, values — в helm/gemini/README.md
helm/examples/      values-source.yaml, values-target.yaml
.github/workflows/  сборка образов и чарта, публикация в ghcr.io по тегу
```

## Локальный запуск

Нужны Go 1.27, bun 1.4, PostgreSQL с `pgcrypto` для метаданных и Keycloak
(или другой OIDC-провайдер) с клиентом, client-ролями `superuser` (и при необходимости `security`) и группами `/<проект>/<admin|developer|readonly>`.
Для запуска прогонов нужен доступ к кластеру — используется `~/.kube/config`;
без него API работает, но оркестрация Job'ов отключена.

Backend:

```bash
cd backend
cp .env.local.example .env.local   # заполнить: POSTGRES_URL, SECRET_ENCRYPTION_KEY (32 байта), OIDC_*, S3_*
set -a; . ./.env.local; set +a

go run ./cmd/gemini migrate        # serve сам миграции не применяет
go run ./cmd/gemini serve          # :8080
go test ./...
```

Frontend:

```bash
cd frontend
cp .env.local.example .env.local   # BACKEND_URL, AUTH_SECRET, KEYCLOAK_*
bun install
bun run dev                        # http://localhost:3000
bun run lint
```

Чтобы проверить связку целиком, поднимите ещё одну копию backend'а с
`HEAD_ROLE=target`, своим `POSTGRES_URL`, `LISTEN_ADDR` и `PUBLIC_URL`.

## Деплой

Каждый инстанс Gemini — отдельный релиз чарта `helm/gemini` со своим `headRole`:

```bash
helm install gemini-source ./helm/gemini -f helm/examples/values-source.yaml -n gemini --create-namespace
helm install gemini-target ./helm/gemini -f helm/examples/values-target.yaml -n gemini --create-namespace
```

Чарт не создаёт БД метаданных, S3-бакет и Secret'ы — они готовятся заранее
(минимум: `POSTGRES_URL`, `SECRET_ENCRYPTION_KEY`, ключи S3,
`KEYCLOAK_CLIENT_SECRET` + `AUTH_SECRET`). Миграции накатывает Job-хук
(`pre-install,pre-upgrade` / Argo CD `PreSync`). Подробности и полный список
values — [helm/README.md](helm/README.md) и
[helm/gemini/README.md](helm/gemini/README.md).

После деплоя инстансов Gemini: для каждого target сделать pairing (source → «Pairing» → код;
target → ввести URL source и код), завести инстансы PostgreSQL, на target — сопоставления
баз.

## Релиз

Пуш тега `vX.Y.Z` собирает и публикует:

- образы `ghcr.io/<repo>/backend:X.Y.Z` и `ghcr.io/<repo>/frontend:X.Y.Z`
  (`latest` — только для релизов без pre-release суффикса);
- чарт `oci://ghcr.io/<repo>/charts/gemini` версии `X.Y.Z` с `appVersion = X.Y.Z`.

В pull request образы только собираются, чарт только линтуется на всех
`helm/examples/values-*.yaml`.
