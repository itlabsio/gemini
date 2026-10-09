// Package config собирает всю рантайм-конфигурацию головы Gemini из окружения.
//
// Обе головы (source и target) используют один и тот же бинарь и один и тот же
// набор переменных; поведение различается полем Role (HEAD_ROLE=source|target).
// target — голова в DR-контуре, куда льётся restore.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Role — роль головы. Определяет, какие фоновые компоненты поднимаются
// (scheduler на source, poller на target) и какие webhook-эндпоинты активны.
type Role string

const (
	RoleSource Role = "source"
	RoleTarget Role = "target"
)

// Config — полная конфигурация процесса.
type Config struct {
	Role       Role
	ListenAddr string

	// PublicURL — внешний URL этой головы, который она сообщает второй голове
	// при pairing (чтобы та знала, куда слать обратные webhook'и).
	PublicURL string

	OwnDatabaseDSN string

	// SecretEncryptionKey — 32-байтный ключ для AES-256-GCM шифрования
	// plain-паролей подключений в собственной БД (at rest).
	SecretEncryptionKey string

	OIDC   OIDCConfig
	S3     S3Config
	Vault  VaultConfig
	Notify NotifyConfig

	// PollInterval — период fallback-поллинга S3 (используется только target-головой).
	PollInterval time.Duration

	PeerRetry PeerRetryConfig

	// K8sNamespace — namespace, в котором голова создаёт Job/CronJob/Secret.
	K8sNamespace string
	// JobImage — образ для dump/restore Job'ов (обычно тот же образ backend'а).
	JobImage string
	// JobServiceAccount — SA, под которым бегают dump/restore-поды.
	JobServiceAccount string
	// JobOwnerDeployment — имя Deployment'а backend'а в этом namespace. Голова
	// читает у него UID (ownerReference для Job/CronJob/Secret), imagePullSecrets
	// и лейбл app.kubernetes.io/instance. Пусто → без ownerRef.
	JobOwnerDeployment string
	// JobStorageClass — storageClass для ephemeral-тома дампа.
	JobStorageClass string
	// JobTimezone — таймзона расписаний CronJob (spec.timeZone).
	JobTimezone string
	// JobStorageType — дефолт временного хранения ("ephemeral" | "emptydir"),
	// когда у базы не задан свой Database.StorageType.
	JobStorageType string
	// JobWorkDirSize — дефолтный размер тома дампа (напр. "20Gi").
	JobWorkDirSize string
	// JobPodOverridesJSON — JSON с nodeSelector / tolerations / resources для
	// dump/restore-подов (и Job, и CronJob). Формат — как в PodSpec k8s.
	JobPodOverridesJSON string
}

// OIDCConfig — параметры проверки JWT от Keycloak.
type OIDCConfig struct {
	IssuerURL string
	// ClientID — единый clientId обеих голов. По нему проверяется azp/aud токена
	// и читаются client-роли из resource_access.<ClientID>.roles (ровно
	// "viewer" / "operator" / "admin"; realm_access.roles и groups не учитываются).
	ClientID string
}

// S3Config — доступ к промежуточному хранилищу дампов.
type S3Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	UseSSL       bool
}

// VaultConfig — клиент HashiCorp Vault для резолва секретов подключений.
type VaultConfig struct {
	Enabled bool
	Address string
	// AuthMethod: "kubernetes" (по SA-токену пода) или "token".
	AuthMethod   string
	Token        string
	K8sRole      string
	K8sMountPath string
	K8sTokenPath string
}

// NotifyConfig — исходящие нотификации об ошибках.
type NotifyConfig struct {
	Enabled    bool
	Provider   string // "slack" | "telegram"
	WebhookURL string
	// TelegramChatID нужен только для provider=telegram.
	TelegramChatID string
}

// PeerRetryConfig — backoff ретраев межголовых webhook'ов (в обе стороны).
type PeerRetryConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
	// ClockSkew — допустимое расхождение часов при проверке HMAC timestamp.
	ClockSkew time.Duration
}

// Load читает конфигурацию из окружения и валидирует обязательные поля.
func Load() (*Config, error) {
	c := &Config{
		Role:                Role(strings.ToLower(env("HEAD_ROLE", "source"))),
		ListenAddr:          env("LISTEN_ADDR", ":8080"),
		PublicURL:           strings.TrimSuffix(os.Getenv("PUBLIC_URL"), "/"),
		OwnDatabaseDSN:      os.Getenv("POSTGRES_URL"),
		SecretEncryptionKey: os.Getenv("SECRET_ENCRYPTION_KEY"),
		OIDC: OIDCConfig{
			IssuerURL: strings.TrimSuffix(os.Getenv("OIDC_ISSUER_URL"), "/"),
			ClientID:  os.Getenv("OIDC_CLIENT_ID"),
		},
		S3: S3Config{
			Endpoint:     env("S3_ENDPOINT", "storage.yandexcloud.net"),
			Region:       env("S3_REGION", "ru-central1"),
			Bucket:       os.Getenv("S3_BUCKET"),
			AccessKey:    os.Getenv("S3_ACCESS_KEY_ID"),
			SecretKey:    os.Getenv("S3_SECRET_ACCESS_KEY"),
			UsePathStyle: envBool("S3_PATH_STYLE", true),
			UseSSL:       envBool("S3_USE_SSL", true),
		},
		Vault: VaultConfig{
			Enabled:      envBool("VAULT_ENABLED", false),
			Address:      os.Getenv("VAULT_ADDR"),
			AuthMethod:   env("VAULT_AUTH_METHOD", "kubernetes"),
			Token:        os.Getenv("VAULT_TOKEN"),
			K8sRole:      os.Getenv("VAULT_K8S_ROLE"),
			K8sMountPath: env("VAULT_K8S_MOUNT", "kubernetes"),
			K8sTokenPath: env("VAULT_K8S_TOKEN_PATH", "/var/run/secrets/kubernetes.io/serviceaccount/token"),
		},
		Notify: NotifyConfig{
			Enabled:        envBool("NOTIFY_ENABLED", false),
			Provider:       env("NOTIFY_PROVIDER", "slack"),
			WebhookURL:     os.Getenv("NOTIFY_WEBHOOK_URL"),
			TelegramChatID: os.Getenv("NOTIFY_TELEGRAM_CHAT_ID"),
		},
		PollInterval: envDuration("POLL_INTERVAL", 5*time.Minute),
		PeerRetry: PeerRetryConfig{
			InitialDelay: envDuration("PEER_RETRY_INITIAL_DELAY", time.Second),
			MaxDelay:     envDuration("PEER_RETRY_MAX_DELAY", 60*time.Second),
			MaxAttempts:  envInt("PEER_RETRY_MAX_ATTEMPTS", 5),
			// Окно приёма межголовой подписи. Внутри него подписанный запрос
			// можно воспроизвести повторно, поэтому держим его узким. Защита от
			// самого повтора — идемпотентность обработчиков (event_key), а не
			// это окно; при расхождении часов между контурами поднимайте
			// PEER_CLOCK_SKEW, но лучше почините NTP.
			ClockSkew: envDuration("PEER_CLOCK_SKEW", 15*time.Second),
		},
		K8sNamespace:        env("K8S_NAMESPACE", "gemini"),
		JobImage:            os.Getenv("JOB_IMAGE"),
		JobServiceAccount:   env("JOB_SERVICE_ACCOUNT", "gemini-job"),
		JobOwnerDeployment:  os.Getenv("JOB_OWNER_DEPLOYMENT"),
		JobStorageClass:     os.Getenv("JOB_STORAGE_CLASS"),
		JobTimezone:         env("JOB_TIMEZONE", "Asia/Yekaterinburg"),
		JobStorageType:      env("JOB_STORAGE_TYPE", "ephemeral"),
		JobWorkDirSize:      env("JOB_WORKDIR_SIZE", "20Gi"),
		JobPodOverridesJSON: os.Getenv("JOB_POD_OVERRIDES"),
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	switch c.Role {
	case RoleSource, RoleTarget:
	default:
		return fmt.Errorf("HEAD_ROLE must be 'source' or 'target', got %q", c.Role)
	}
	if c.OwnDatabaseDSN == "" {
		return fmt.Errorf("POSTGRES_URL is required")
	}
	if len(c.SecretEncryptionKey) != 32 {
		return fmt.Errorf("SECRET_ENCRYPTION_KEY must be exactly 32 bytes (got %d)", len(c.SecretEncryptionKey))
	}
	if c.OIDC.IssuerURL == "" || c.OIDC.ClientID == "" {
		return fmt.Errorf("OIDC_ISSUER_URL and OIDC_CLIENT_ID are required")
	}
	if c.Vault.Enabled {
		if c.Vault.Address == "" {
			return fmt.Errorf("VAULT_ADDR is required when VAULT_ENABLED=true")
		}
		// Раньше любое незнакомое значение молча уходило в kubernetes-ветку и
		// логинилось в auth/kubernetes/login — а readiness падала без внятной
		// причины. Частая путаница: имя auth-бэкенда пишут сюда, хотя оно
		// задаётся отдельно, в VAULT_K8S_MOUNT.
		switch c.Vault.AuthMethod {
		case "kubernetes":
			if c.Vault.K8sRole == "" {
				return fmt.Errorf("VAULT_K8S_ROLE is required for VAULT_AUTH_METHOD=kubernetes")
			}
		case "token":
			if c.Vault.Token == "" {
				return fmt.Errorf("VAULT_TOKEN is required for VAULT_AUTH_METHOD=token")
			}
		default:
			return fmt.Errorf("VAULT_AUTH_METHOD must be 'kubernetes' or 'token', got %q "+
				"(the mount path of the kubernetes auth backend goes to VAULT_K8S_MOUNT, not here)",
				c.Vault.AuthMethod)
		}
	}
	if c.Notify.Enabled && c.Notify.WebhookURL == "" {
		return fmt.Errorf("NOTIFY_WEBHOOK_URL is required when NOTIFY_ENABLED=true")
	}
	return nil
}

func (c *Config) IsSource() bool { return c.Role == RoleSource }
func (c *Config) IsTarget() bool { return c.Role == RoleTarget }

// PeerRole — роль второй головы относительно этой. Пар в системе ровно две,
// поэтому роль пира однозначно выводится из собственной и НЕ должна браться из
// заголовка входящего запроса: иначе клиент выбирал бы, каким секретом его же
// подпись будут проверять.
func (c *Config) PeerRole() string {
	if c.IsSource() {
		return string(RoleTarget)
	}
	return string(RoleSource)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
