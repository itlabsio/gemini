// Package model описывает доменные сущности СРК Gemini (раздел 3 плана).
// Структура одинакова для обеих голов; БД у каждой головы своя.
package model

import (
	"encoding/json"
	"time"
)

// AuthType — способ хранения секрета подключения к Instance.
type AuthType string

const (
	AuthPlain AuthType = "plain"
	AuthVault AuthType = "vault"
)

// InstanceRole — роль сервера БД относительно СРК.
type InstanceRole string

const (
	InstanceSource InstanceRole = "source"
	InstanceTarget InstanceRole = "target"
)

// RestoreMode — стратегия restore для target-инстанса.
type RestoreMode string

const (
	// RestoreRecreate — RENAME→CREATE→накат→DROP old (self-hosted).
	RestoreRecreate RestoreMode = "recreate"
	// RestoreInPlace — накат --clean --if-exists в существующую базу (MDB).
	RestoreInPlace RestoreMode = "in_place"
)

// Instance — сервер PostgreSQL (managed source или self-hosted target).
type Instance struct {
	ID       string       `json:"id"`
	Role     InstanceRole `json:"role"`
	Name     string       `json:"name"`
	Host     string       `json:"host"`
	Port     int          `json:"port"`
	AuthType AuthType     `json:"auth_type"`

	// RestoreMode — как target-голова восстанавливает базы этого инстанса.
	// Пусто в API-ответе для source-инстансов. См. RestoreMode.
	RestoreMode RestoreMode `json:"restore_mode,omitempty"`

	// PlainUsername виден в API; пароль наружу не отдаётся никогда.
	PlainUsername string `json:"plain_username,omitempty"`

	VaultPath string `json:"vault_path,omitempty"`
	VaultRole string `json:"vault_role,omitempty"`

	// VaultUsernameKey / VaultPasswordKey — какие поля секрета считать логином и
	// паролем. Пусто → эвристика по известным именам (username/user/login и т.п.).
	VaultUsernameKey string `json:"vault_username_key,omitempty"`
	VaultPasswordKey string `json:"vault_password_key,omitempty"`

	SSLMode string `json:"ssl_mode"`

	// SSLRootCert — PEM кастомного корневого CA для self-hosted инстанса с
	// собственным сертификатом. Пусто → системный trust store (Yandex Cloud CA +
	// публичные). Применяется только при ssl_mode verify-ca/verify-full.
	SSLRootCert string `json:"ssl_root_cert,omitempty"`

	// DiscoveryDB — база, к которой подключается discovery для чтения pg_database.
	// Пусто → "postgres". Для Yandex Managed PostgreSQL обязательно указать
	// существующую пользовательскую базу (базы postgres в пуле нет).
	DiscoveryDB string `json:"discovery_db,omitempty"`

	// ExcludedDatabases — базы, которые discovery игнорирует. postgres
	// пропускается всегда и без этого списка.
	ExcludedDatabases []string `json:"excluded_databases"`

	// ServerVersionNum — server_version_num инстанса по последнему discovery
	// (напр. 170004 = PG 17.4). 0 → discovery ещё не выполнялся.
	ServerVersionNum int `json:"server_version_num,omitempty"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StorageType — где Job dump/restore держит временный файл дампа.
type StorageType string

const (
	// StorageEphemeral — generic ephemeral volume (PVC на время жизни пода),
	// storageClass должен быть с volumeBindingMode: WaitForFirstConsumer.
	StorageEphemeral StorageType = "ephemeral"
	// StorageEmptyDir — emptyDir (диск ноды / tmpfs), без провижининга PVC.
	StorageEmptyDir StorageType = "emptydir"
)

// Database — конкретная база на Instance, обнаруженная через discovery.
type Database struct {
	ID         string `json:"id"`
	InstanceID string `json:"instance_id"`
	DBName     string `json:"db_name"`
	Enabled    bool   `json:"enabled"`

	// ScheduleCron заполняется только на Source.
	ScheduleCron string `json:"schedule_cron,omitempty"`

	// StorageType / StorageSize — временное хранение дампа для Job'ов этой базы.
	// Пустые значения → дефолты головы (JOB_STORAGE_TYPE / JOB_WORKDIR_SIZE).
	StorageType StorageType `json:"storage_type,omitempty"`
	StorageSize string      `json:"storage_size,omitempty"`

	// ExternalID — стабильный идентификатор, которым Source называет базу
	// во всех межголовых API/webhook-вызовах.
	ExternalID string `json:"external_id"`

	// Present=false — база пропала из discovery, но запись оставлена для истории.
	Present bool `json:"present"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PairingStatus — состояние доверия между головами.
type PairingStatus string

const (
	PairingPending PairingStatus = "pending"
	PairingActive  PairingStatus = "active"
	PairingRevoked PairingStatus = "revoked"
)

// PairingSecret — HMAC-секрет активной пары вместе с её id. Верификатор
// межголовых запросов перебирает секреты всех пиров роли.
type PairingSecret struct {
	PairingID string
	Secret    string
}

// HeadPairing — запись о доверии между конкретными Source и DR.
// Секрет (auth_secret) в API/JSON не отдаётся — хранится только его материал.
type HeadPairing struct {
	ID       string        `json:"id"`
	PeerRole string        `json:"peer_role"` // 'source' видит 'dr'-пира и наоборот
	PeerURL  string        `json:"peer_url"`
	Status   PairingStatus `json:"status"`

	// PeerPublicKey — публичный ключ пира (base64 Ed25519), запиненный при
	// pairing. Наружу в JSON не отдаётся.
	PeerPublicKey string `json:"-"`

	EstablishedAt *time.Time `json:"established_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// DatabaseMapping — живёт на DR: сопоставление source-базы и локальной target-базы.
type DatabaseMapping struct {
	ID                       string `json:"id"`
	HeadPairingID            string `json:"head_pairing_id"`
	SourceDatabaseExternalID string `json:"source_database_external_id"`
	SourceDBName             string `json:"source_db_name"`
	TargetDatabaseID         string `json:"target_database_id"`
	// TargetOwner — заранее созданная роль на target-кластере, которой будет
	// принадлежать восстановленная база (restore идёт под суперюзером).
	TargetOwner string `json:"target_owner"`
	S3Prefix    string `json:"s3_prefix"`
	Enabled     bool   `json:"enabled"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// S3Bucket — бакет дампов. Дампы заливаются во все включённые бакеты (fan-out),
// restore берёт дамп из первого бакета, где он есть.
type S3Bucket struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	Bucket   string `json:"bucket"`

	AccessKeyID string `json:"access_key_id"`
	// SecretAccessKey наружу не отдаётся никогда.
	SecretAccessKey string `json:"-"`

	PathStyle bool `json:"path_style"`
	UseSSL    bool `json:"use_ssl"`
	Enabled   bool `json:"enabled"`

	// BuiltIn — бакет из env (Helm values.s3). В UI только для чтения.
	BuiltIn bool `json:"built_in"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RunKind / RunTrigger / RunStatus — измерения записи истории BackupRun.
type (
	RunKind    string
	RunTrigger string
	RunStatus  string
)

const (
	KindDump    RunKind = "dump"
	KindRestore RunKind = "restore"

	TriggerScheduled    RunTrigger = "scheduled"
	TriggerManual       RunTrigger = "manual"
	TriggerWebhook      RunTrigger = "webhook"
	TriggerPollFallback RunTrigger = "poll-fallback"

	StatusPending   RunStatus = "pending"
	StatusRunning   RunStatus = "running"
	StatusSucceeded RunStatus = "succeeded"
	StatusFailed    RunStatus = "failed"
)

// BackupRun — одна попытка dump или restore.
type BackupRun struct {
	ID          string     `json:"id"`
	DatabaseID  string     `json:"database_id"`
	Kind        RunKind    `json:"kind"`
	Trigger     RunTrigger `json:"trigger"`
	Status      RunStatus  `json:"status"`
	K8sJobName  string     `json:"k8s_job_name,omitempty"`
	S3ObjectKey string     `json:"s3_object_key,omitempty"`
	Checksum    string     `json:"checksum,omitempty"`

	// EventKey — бизнес-ключ события для идемпотентности обработчиков webhook/poll.
	EventKey string `json:"-"`

	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	InitiatedBy  string     `json:"initiated_by"`

	// PeerStatuses — исход применения этого дампа каждым Target-контуром
	// (Source заполняет из обратных webhook'ов restore-status). Пусто на Target.
	PeerStatuses []RunPeerStatus `json:"peer_statuses,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// RunPeerStatus — исход restore одного дампа в конкретном Target-контуре.
type RunPeerStatus struct {
	PeerURL    string    `json:"peer_url"`
	Status     RunStatus `json:"status"`
	Error      string    `json:"error,omitempty"`
	ReportedAt time.Time `json:"reported_at"`
}

// Active сообщает, идёт ли ещё этот прогон (для проверки конкурентности).
func (r BackupRun) Active() bool {
	return r.Status == StatusPending || r.Status == StatusRunning
}

// ResourceQuantities — cpu/memory для одной стороны (requests или limits).
type ResourceQuantities struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// ResourceSpec — requests/limits контейнера.
type ResourceSpec struct {
	Requests ResourceQuantities `json:"requests,omitempty"`
	Limits   ResourceQuantities `json:"limits,omitempty"`
}

// Settings — глобальные дефолты СРК, редактируемые из UI (админка).
// Применяются, когда у конкретной базы значение не задано.
type Settings struct {
	DefaultStorageType StorageType  `json:"default_storage_type"`
	DefaultStorageSize string       `json:"default_storage_size"`
	DefaultResources   ResourceSpec `json:"default_resources"`
	// DefaultPodScheduling — nodeSelector / tolerations / affinity dump/restore-подов
	// (JSON как в PodSpec). Перекрывает values чарта. Пусто → значения чарта.
	DefaultPodScheduling json.RawMessage `json:"default_pod_scheduling,omitempty"`
	// JobTTLMinutes — через сколько минут после завершения k8s удаляет
	// dump/restore-Job вместе с подами (spec.ttlSecondsAfterFinished).
	JobTTLMinutes int32     `json:"job_ttl_minutes"`
	UpdatedAt     time.Time `json:"updated_at"`
	UpdatedBy     string    `json:"updated_by"`
}

// User — кэш последних логинов для аудита UI.
type User struct {
	Subject    string    `json:"subject"`
	Email      string    `json:"email"`
	Roles      []string  `json:"roles"`
	LastSeenAt time.Time `json:"last_seen_at"`
}
