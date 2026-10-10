import { auth } from "@/lib/auth";
import type {
  BackupRun,
  Candidate,
  Database,
  DatabaseMapping,
  HeadPairing,
  HeadRole,
  Instance,
  Me,
  ResourceSpec,
  S3Bucket,
  Settings,
} from "@/lib/types";

const API_URL = process.env.BACKEND_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
    public readonly body?: unknown,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function accessToken(): Promise<string> {
  const session = await auth();
  if (!session?.accessToken) throw new ApiError(401, "no session");
  return session.accessToken;
}

async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = await accessToken();
  const res = await fetch(`${API_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...init.headers,
    },
    cache: "no-store",
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText, body);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// --- чтение ---

/**
 * Публичная конфигурация головы — без авторизации. Нужна шеллу (бейдж роли
 * source/target), чтобы он был верным ещё на странице логина, до токена.
 */
export const getPublicConfig = async (): Promise<{ head_role: HeadRole }> => {
  const res = await fetch(`${API_URL}/api/config`, { cache: "no-store" });
  if (!res.ok) throw new ApiError(res.status, res.statusText);
  return (await res.json()) as { head_role: HeadRole };
};

export const getMe = () => apiFetch<Me>("/api/me");
export const listInstances = () => apiFetch<Instance[]>("/api/instances");
export const getInstance = (id: string) => apiFetch<Instance>(`/api/instances/${id}`);
export const listDatabases = (instanceId: string) =>
  apiFetch<Database[]>(`/api/instances/${instanceId}/databases`);
export const listRuns = (qs = "") => apiFetch<BackupRun[]>(`/api/runs${qs}`);
export const listActiveRuns = () => apiFetch<BackupRun[]>("/api/runs/active");
export const cancelRun = (id: string) =>
  apiFetch<BackupRun>(`/api/runs/${id}/cancel`, { method: "POST" });
export const listPairings = () => apiFetch<HeadPairing[]>("/api/pairings");
export const deletePairing = (id: string) =>
  apiFetch<void>(`/api/pairings/${id}`, { method: "DELETE" });
export const revokePairing = (id: string) =>
  apiFetch<HeadPairing>(`/api/pairings/${id}/revoke`, { method: "POST" });
export const listMappings = () => apiFetch<DatabaseMapping[]>("/api/mapping");
export const mappingCandidates = () =>
  apiFetch<{ pairing_id: string; candidates: Candidate[] }>("/api/mapping/candidates");

// --- запись ---
export interface InstancePayload {
  role?: string;
  name: string;
  host: string;
  port: number;
  ssl_mode: string;
  ssl_root_cert?: string;
  discovery_db?: string;
  restore_mode?: string;
  auth_type: string;
  plain_username?: string;
  plain_password?: string;
  vault_path?: string;
  vault_role?: string;
  vault_username_key?: string;
  vault_password_key?: string;
  excluded_databases?: string[];
}

export const createInstance = (p: InstancePayload) =>
  apiFetch<Instance>("/api/instances", { method: "POST", body: JSON.stringify(p) });
export const updateInstance = (id: string, p: InstancePayload) =>
  apiFetch<Instance>(`/api/instances/${id}`, { method: "PUT", body: JSON.stringify(p) });
export const deleteInstance = (id: string) =>
  apiFetch<void>(`/api/instances/${id}`, { method: "DELETE" });
export const discoverInstance = (id: string) =>
  apiFetch<{ databases: Database[]; server_version_num: number }>(
    `/api/instances/${id}/discover`,
    { method: "POST" },
  );
/** Имена ролей target-кластера — кандидаты на владельца восстановленной базы. */
export const listInstanceRoles = (id: string) =>
  apiFetch<string[]>(`/api/instances/${id}/roles`);
export const patchDatabase = (
  id: string,
  patch: {
    enabled?: boolean;
    schedule_cron?: string;
    storage_type?: string;
    storage_size?: string;
  }
) => apiFetch<Database>(`/api/databases/${id}`, { method: "PATCH", body: JSON.stringify(patch) });

/** Имена полей секрета Vault для сопоставления в UI. Значения не возвращаются. */
export const vaultSecretKeys = (path: string) =>
  apiFetch<{
    keys: string[];
    guess_username_key: string;
    guess_password_key: string;
  }>("/api/vault/secret-keys", { method: "POST", body: JSON.stringify({ path }) });

export const getSettings = () => apiFetch<Settings>("/api/settings");
export const updateSettings = (p: {
  default_storage_type: string;
  default_storage_size: string;
  default_resources: ResourceSpec;
  default_pod_scheduling?: Record<string, unknown> | null;
  job_ttl_minutes: number;
  job_pod_start_timeout_minutes: number;
}) => apiFetch<Settings>("/api/settings", { method: "PUT", body: JSON.stringify(p) });

export const listS3Buckets = () => apiFetch<S3Bucket[]>("/api/s3-buckets");
export interface S3BucketPayload {
  name: string;
  endpoint: string;
  region: string;
  bucket: string;
  access_key_id: string;
  /** Пусто при обновлении — оставить сохранённый */
  secret_access_key: string;
  path_style: boolean;
  use_ssl: boolean;
  enabled: boolean;
}
export const createS3Bucket = (p: S3BucketPayload) =>
  apiFetch<S3Bucket>("/api/s3-buckets", { method: "POST", body: JSON.stringify(p) });
export const updateS3Bucket = (id: string, p: S3BucketPayload) =>
  apiFetch<S3Bucket>(`/api/s3-buckets/${id}`, { method: "PUT", body: JSON.stringify(p) });
export const deleteS3Bucket = (id: string) =>
  apiFetch<void>(`/api/s3-buckets/${id}`, { method: "DELETE" });
export const testS3Bucket = (id: string) =>
  apiFetch<{ status: string }>(`/api/s3-buckets/${id}/test`, { method: "POST" });

export const runDatabase = (id: string) =>
  apiFetch<BackupRun>(`/api/databases/${id}/run`, { method: "POST" });

export const issuePairingCode = () =>
  apiFetch<{ code: string; expires_in_seconds: number }>("/api/pairing/issue-code", {
    method: "POST",
  });
export const initPairing = (source_url: string, code: string) =>
  apiFetch<HeadPairing>("/api/pairing/init", {
    method: "POST",
    body: JSON.stringify({ source_url, code }),
  });

export interface MappingPayload {
  head_pairing_id: string;
  source_database_external_id: string;
  source_db_name: string;
  target_database_id: string;
  target_owner: string;
  s3_prefix: string;
  enabled: boolean;
}
export const saveMapping = (p: MappingPayload) =>
  apiFetch<DatabaseMapping>("/api/mapping", { method: "POST", body: JSON.stringify(p) });
