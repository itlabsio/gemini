"use server";

import { revalidatePath } from "next/cache";
import {
  ApiError,
  cancelRun,
  createInstance,
  deleteInstance,
  discoverInstance,
  listActiveRuns,
  patchDatabase,
  runDatabase,
  updateInstance,
  vaultSecretKeys,
  type InstancePayload,
} from "@/lib/api";
import type { BackupRun } from "@/lib/types";

type Result = { ok: true } | { ok: false; error: string };

function toResult(e: unknown): Result {
  if (e instanceof ApiError) return { ok: false, error: `${e.status}: ${e.message}` };
  return { ok: false, error: e instanceof Error ? e.message : "неизвестная ошибка" };
}

/**
 * Имена полей секрета Vault по пути — для сопоставления их с логином и паролем.
 * Значения полей бэкенд не отдаёт, только имена.
 */
export async function vaultSecretKeysAction(path: string): Promise<
  | { ok: true; keys: string[]; guessUsernameKey: string; guessPasswordKey: string }
  | { ok: false; error: string }
> {
  try {
    const res = await vaultSecretKeys(path);
    return {
      ok: true,
      keys: res.keys,
      guessUsernameKey: res.guess_username_key,
      guessPasswordKey: res.guess_password_key,
    };
  } catch (e) {
    const r = toResult(e);
    return { ok: false, error: r.ok ? "неизвестная ошибка" : r.error };
  }
}

export async function createInstanceAction(p: InstancePayload): Promise<Result & { id?: string }> {
  try {
    const inst = await createInstance(p);
    revalidatePath("/instances");
    return { ok: true, id: inst.id };
  } catch (e) {
    return toResult(e);
  }
}

export async function updateInstanceAction(id: string, p: InstancePayload): Promise<Result> {
  try {
    await updateInstance(id, p);
    revalidatePath(`/instances/${id}`);
    return { ok: true };
  } catch (e) {
    return toResult(e);
  }
}

export async function deleteInstanceAction(id: string): Promise<Result> {
  try {
    await deleteInstance(id);
    revalidatePath("/instances");
    return { ok: true };
  } catch (e) {
    return toResult(e);
  }
}

export async function discoverAction(
  id: string,
): Promise<(Result & { serverVersionNum?: number }) | { ok: false; error: string }> {
  try {
    const res = await discoverInstance(id);
    revalidatePath(`/instances/${id}`);
    return { ok: true, serverVersionNum: res.server_version_num };
  } catch (e) {
    return toResult(e);
  }
}

export async function patchDatabaseAction(
  instanceId: string,
  databaseId: string,
  patch: {
    enabled?: boolean;
    schedule_cron?: string;
    storage_type?: string;
    storage_size?: string;
  }
): Promise<Result> {
  try {
    await patchDatabase(databaseId, patch);
    revalidatePath(`/instances/${instanceId}`);
    return { ok: true };
  } catch (e) {
    return toResult(e);
  }
}

/** Активные (pending/running) прогоны — для опроса таблицей баз. */
export async function activeRunsAction(): Promise<
  { ok: true; runs: BackupRun[] } | { ok: false; error: string }
> {
  try {
    return { ok: true, runs: await listActiveRuns() };
  } catch (e) {
    const r = toResult(e);
    return { ok: false, error: r.ok ? "неизвестная ошибка" : r.error };
  }
}

/** Остановка активного прогона: бэкенд удаляет Job и закрывает прогон. */
export async function cancelRunAction(instanceId: string, runId: string): Promise<Result> {
  try {
    await cancelRun(runId);
    revalidatePath(`/instances/${instanceId}`);
    return { ok: true };
  } catch (e) {
    return toResult(e);
  }
}

export async function runDatabaseAction(instanceId: string, databaseId: string): Promise<Result> {
  try {
    await runDatabase(databaseId);
    revalidatePath(`/instances/${instanceId}`);
    return { ok: true };
  } catch (e) {
    return toResult(e);
  }
}
