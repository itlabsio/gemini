export type HeadRole = "source" | "target";
export type AuthType = "plain" | "vault";
export type InstanceRole = "source" | "target";
export type StorageType = "ephemeral" | "emptydir";
export type RestoreMode = "recreate" | "in_place";

export interface Instance {
  id: string;
  role: InstanceRole;
  name: string;
  host: string;
  port: number;
  auth_type: AuthType;
  plain_username?: string;
  vault_path?: string;
  vault_role?: string;
  vault_username_key?: string;
  vault_password_key?: string;
  ssl_mode: string;
  ssl_root_cert?: string;
  discovery_db?: string;
  excluded_databases?: string[];
  /** server_version_num по последнему discovery (напр. 170004); 0/undefined — не выполнялся. */
  server_version_num?: number;
  /** Стратегия restore (только target-инстансы). */
  restore_mode?: RestoreMode;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface Database {
  id: string;
  instance_id: string;
  db_name: string;
  enabled: boolean;
  schedule_cron?: string;
  storage_type?: "" | StorageType;
  storage_size?: string;
  external_id: string;
  present: boolean;
  created_at: string;
  updated_at: string;
}

export interface ResourceQuantities {
  cpu?: string;
  memory?: string;
}
export interface ResourceSpec {
  requests?: ResourceQuantities;
  limits?: ResourceQuantities;
}
export interface Settings {
  default_storage_type: StorageType;
  default_storage_size: string;
  default_resources: ResourceSpec;
  default_pod_scheduling?: Record<string, unknown>;
  /** Через сколько минут после завершения k8s удаляет dump/restore-Job */
  job_ttl_minutes: number;
  /** Сколько минут ждать старта пода Job'а, потом прогон → failed */
  job_pod_start_timeout_minutes: number;
  updated_at: string;
  updated_by: string;
}

/** S3-бакет дампов. Дампы заливаются во все включённые бакеты */
export interface S3Bucket {
  id: string;
  name: string;
  endpoint: string;
  region: string;
  bucket: string;
  access_key_id: string;
  path_style: boolean;
  use_ssl: boolean;
  enabled: boolean;
  /** Бакет из Helm values (s3.*) — только для чтения */
  built_in: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export type RunKind = "dump" | "restore";
export type RunStatus = "pending" | "running" | "succeeded" | "failed";

export interface BackupRun {
  id: string;
  database_id: string;
  kind: RunKind;
  trigger: string;
  status: RunStatus;
  k8s_job_name?: string;
  s3_object_key?: string;
  checksum?: string;
  started_at?: string;
  finished_at?: string;
  error_message?: string;
  initiated_by: string;
  /** Исход restore этого дампа по каждому Target-контуру (только на Source). */
  peer_statuses?: RunPeerStatus[];
  created_at: string;
}

export interface RunPeerStatus {
  peer_url: string;
  status: RunStatus;
  error?: string;
  reported_at: string;
}

export interface HeadPairing {
  id: string;
  peer_role: string;
  peer_url: string;
  status: "pending" | "active" | "revoked";
  established_at?: string;
  created_at: string;
}

export interface DatabaseMapping {
  id: string;
  head_pairing_id: string;
  source_database_external_id: string;
  source_db_name: string;
  target_database_id: string;
  /** Роль-владелец восстановленной базы на target-кластере. */
  target_owner: string;
  s3_prefix: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Candidate {
  external_id: string;
  db_name: string;
  /** Префикс дампов этой базы в S3, как его формирует Source (<инстанс>/<база>). */
  s3_prefix: string;
}

export interface Me {
  subject: string;
  email: string;
  role: "viewer" | "operator" | "admin";
  head_role: HeadRole;
  vault_enabled: boolean;
  /** Публичный ключ этой головы (base64 Ed25519) — для сверки при pairing. */
  head_public_key: string;
}
