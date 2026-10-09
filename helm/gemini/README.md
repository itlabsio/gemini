# gemini

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

СРК Gemini — одна "голова" (backend + frontend) DR-системы логической репликации PostgreSQL. Роль (source|dr) задаётся через values.headRole.

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| devops |  |  |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| headRole | string | `"source"` | Роль головы: source (текущий контур, инициатор расписания) | target (DR-контур, restore) |
| nameOverride | string | `""` | Переопределяет имя в шаблонах (app.kubernetes.io/name); пусто = Chart.Name |
| fullnameOverride | string | `""` | Переопределяет префикс имён ресурсов (Deployment/Service/Ingress/...); пусто = Release.Name |
| image.backend.repository | string | `"ghcr.io/itlabsio/gemini/backend"` | Репозиторий образа backend |
| image.backend.tag | string | `""` | Тег. Пусто = appVersion чарта |
| image.backend.pullPolicy | string | `"IfNotPresent"` | Политика загрузки образа backend |
| image.frontend.repository | string | `"ghcr.io/itlabsio/gemini/frontend"` | Репозиторий образа frontend |
| image.frontend.tag | string | `""` | Тег. Пусто = appVersion чарта |
| image.frontend.pullPolicy | string | `"IfNotPresent"` | Политика загрузки образа frontend |
| imagePullSecrets | list | `[]` | Секреты для доступа к приватному registry (imagePullSecrets пода) |
| publicUrl | string | `""` | Внешний URL этой головы (нужен второй голове для обратных webhook'ов).    Пусто и включён ingress с host -> выводится автоматически: https://host (tls) | http://host.    Задавать вручную только если ingress выключен или пиру нужен другой хостнейм. |
| backend.replicas | int | `1` | Число реплик backend |
| backend.revisionHistoryLimit | int | `1` | Сколько старых ReplicaSet'ов хранить для отката |
| backend.extraEnv | list | `[]` | Дополнительные env (несекретные) |
| backend.resources | object | `{"limits":{"cpu":"1","memory":"512Mi"},"requests":{"cpu":"50m","memory":"128Mi"}}` | requests/limits контейнера backend |
| backend.podAnnotations | object | `{}` | Дополнительные аннотации пода backend |
| backend.nodeSelector | object | `{}` | nodeSelector пода backend |
| backend.tolerations | list | `[]` | tolerations пода backend |
| backend.affinity | object | `{}` | affinity пода backend |
| backend.podSecurityContext | object | `{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001,"seccompProfile":{"type":"RuntimeDefault"}}` | securityContext пода backend. uid/gid зашиты в образе (10001). |
| frontend.replicas | int | `1` | Число реплик frontend |
| frontend.revisionHistoryLimit | int | `1` | Сколько старых ReplicaSet'ов хранить для отката |
| frontend.extraEnv | list | `[]` | Дополнительные env (несекретные) |
| frontend.resources | object | `{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"25m","memory":"128Mi"}}` | requests/limits контейнера frontend |
| frontend.podAnnotations | object | `{}` | Дополнительные аннотации пода frontend |
| frontend.nodeSelector | object | `{}` | nodeSelector пода frontend |
| frontend.tolerations | list | `[]` | tolerations пода frontend |
| frontend.affinity | object | `{}` | affinity пода frontend |
| frontend.podSecurityContext | object | `{"fsGroup":1001,"runAsGroup":1001,"runAsNonRoot":true,"runAsUser":1001,"seccompProfile":{"type":"RuntimeDefault"}}` | securityContext пода frontend. uid/gid зашиты в образе Next.js (1001). |
| oidc.issuerUrl | string | `"https://keycloak.example.com/realms/gemini"` | URL issuer'а OIDC (realm Keycloak) |
| oidc.clientId | string | `"gemini"` | Единый clientId frontend и backend: фронт логинится через него (code+PKCE),    backend проверяет по нему azp/aud и читает resource_access.<clientId>.roles    (client-роли ровно viewer / operator / admin). |
| oidc.existingSecret | string | `"gemini-oidc"` | k8s Secret для frontend (next-auth) с ключами:    KEYCLOAK_CLIENT_SECRET, AUTH_SECRET. Backend этот секрет не использует. |
| ownDatabase.existingSecret | string | `"gemini-db-credentials"` | k8s Secret с ключом POSTGRES_URL (dsn собственной БД метаданных).    Чарт свою БД НЕ разворачивает — ожидается внешний PostgreSQL с расширением    pgcrypto (роль dsn должна иметь право CREATE EXTENSION либо pgcrypto    предустановлен). Миграции накатывает отдельный Job (helm hook). |
| encryptionKey | object | `{"existingSecret":"gemini-encryption-key"}` | k8s Secret с ключом SECRET_ENCRYPTION_KEY (ровно 32 байта) для шифрования    plain-паролей подключений в собственной БД at rest. |
| s3.endpoint | string | `"storage.yandexcloud.net"` | Endpoint S3-совместимого хранилища дампов |
| s3.region | string | `"ru-central1"` | Регион S3 |
| s3.bucket | string | `"gemini-dumps"` | Бакет для дампов |
| s3.pathStyle | bool | `true` | Path-style адресация бакета (вместо virtual-hosted) |
| s3.useSSL | bool | `true` | Использовать HTTPS при обращении к S3 |
| s3.existingSecret | string | `"gemini-s3-credentials"` | k8s Secret с ключами S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY |
| vault.enabled | bool | `false` | Включить интеграцию с Vault для секретов подключений |
| vault.addr | string | `"https://vault.example.com:8200"` | Адрес Vault |
| vault.authMethod | string | `"kubernetes"` | Метод аутентификации: kubernetes (по SA-токену пода) | token.    Это НЕ имя auth-бэкенда в Vault — оно задаётся в k8sMount ниже. |
| vault.k8sMount | string | `"kubernetes"` | Путь монтирования kubernetes auth-бэкенда в Vault (`vault auth list`).    Дефолт Vault — "kubernetes"; в инсталляциях его часто монтируют иначе. |
| vault.role | string | `"gemini"` | Vault-роль, на которую логинится SA backend'а |
| vault.existingSecret | string | `""` | нужен только при authMethod=token; k8s Secret с ключом VAULT_TOKEN |
| vault.saAnnotations | object | `{}` | аннотации для job-ServiceAccount (например, для Vault Agent Injector) |
| notify.enabled | bool | `false` | Включить отправку уведомлений о результатах dump/restore |
| notify.provider | string | `"slack"` | slack | telegram |
| notify.existingSecret | string | `"gemini-notify"` | k8s Secret с ключом NOTIFY_WEBHOOK_URL (+ NOTIFY_TELEGRAM_CHAT_ID для telegram) |
| poller.interval | string | `"5m"` | Период fallback-поллинга S3 (только target-голова) |
| peerRetry.initialDelay | string | `"1s"` | Начальная задержка ретрая межголовых запросов |
| peerRetry.maxDelay | string | `"60s"` | Максимальная задержка ретрая (экспоненциальный backoff) |
| peerRetry.maxAttempts | int | `5` | Максимальное число попыток |
| peerRetry.clockSkew | string | `"30s"` | Допустимое расхождение часов между головами |
| migrate.enabled | bool | `true` | Запускать миграции своей БД метаданных как Job (helm hook post-install,post-upgrade) |
| migrate.backoffLimit | int | `3` | backoffLimit Job'а миграции |
| job.serviceAccount | string | `"gemini-job"` | SA, под которым бегают dump/restore-поды (создаётся при rbac.create) |
| job.image | string | `""` | образ Job'ов; пусто = image.backend (тот же бинарь, подкоманды dump/restore) |
| job.timezone | string | `"Asia/Yekaterinburg"` | Таймзона расписаний CronJob (spec.timeZone) |
| job.storage.type | string | `"ephemeral"` | Дефолтный тип временного хранилища дампа: ephemeral (generic ephemeral PVC) | emptydir.    Переопределяется в UI (админка) и на каждую базу отдельно. |
| job.storage.size | string | `"20Gi"` | Дефолтный размер тома дампа |
| job.storage.className | string | `"yc-network-ssd"` | storageClass для ephemeral-тома (нужен volumeBindingMode: WaitForFirstConsumer) |
| job.nodeSelector | object | `{}` | nodeSelector dump/restore-подов (Job и CronJob) |
| job.tolerations | list | `[]` | tolerations dump/restore-подов |
| job.affinity | object | `{}` | affinity dump/restore-подов |
| job.resources | object | `{}` | requests/limits dump/restore-контейнера. {} = встроенные дефолты бэкенда    (500m/512Mi .. 2/2Gi). Можно менять и в UI (админка). |
| rbac.create | bool | `true` | Создавать ServiceAccount + namespaced Role/RoleBinding |
| rbac.storageClassRead | bool | `true` | Создавать ClusterRole/ClusterRoleBinding на чтение storageclasses |
| rbac.serviceAccountName | string | `"gemini"` | Имя ServiceAccount backend-пода |
| service.type | string | `"ClusterIP"` | Тип Service (ClusterIP | NodePort | LoadBalancer) |
| service.backendPort | int | `8080` | Порт Service backend |
| service.frontendPort | int | `3000` | Порт Service frontend |
| ingress.enabled | bool | `false` | Создавать Ingress для головы |
| ingress.className | string | `"nginx"` | ingressClassName |
| ingress.annotations | object | `{"nginx.ingress.kubernetes.io/limit-burst-multiplier":"5","nginx.ingress.kubernetes.io/limit-connections":"20","nginx.ingress.kubernetes.io/limit-rps":"20","nginx.ingress.kubernetes.io/proxy-buffer-size":"8k"}` | Аннотации ingress.    proxy-buffer-size поднят под Set-Cookie next-auth (дефолтных 4k не    хватает: "upstream sent too big header").    limit-* — рейт-лимит: /pairing/exchange, /peer и /webhook доступны из    интернета по определению (вторая голова живёт в другом контуре), а само    приложение лимитировать по IP не может — за ingress все клиентские адреса    одинаковые, а X-Forwarded-For подделывается. Значения под интерактивную    админку: межголовых запросов единицы в час. |
| ingress.host | string | `"gemini.example.com"` | Хост головы. /api/auth → frontend; /api, /peer, /webhook, /pairing/exchange → backend; / → frontend.    При enabled=true и заданном host отсюда выводится publicUrl (если он не задан явно). |
| ingress.tls | bool | `true` | Включить TLS на ingress |
| ingress.tlsSecret | string | `""` | Имя k8s Secret с TLS-сертификатом; пусто = <host>-tls |
| networkPolicy.enabled | bool | `false` | Ограничить исходящий трафик backend'а (DNS + HTTPS + PostgreSQL). Точечная настройка — на стороне кластера. |
| networkPolicy.extraEgressPorts | list | `[]` | Дополнительные порты egress сверх встроенных (DNS/443/6443/5432/6432).    Список объектов k8s NetworkPolicyPort, например:    [{protocol: TCP, port: 5433}, {protocol: TCP, port: 9000}] |
| containerSecurityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | securityContext контейнеров (общий для backend/frontend/job — без uid/gid) |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
