"use server";

import { revalidatePath } from "next/cache";
import {
  ApiError,
  createS3Bucket,
  deleteS3Bucket,
  testS3Bucket,
  updateS3Bucket,
  updateSettings,
  type S3BucketPayload,
} from "@/lib/api";
import type { ResourceSpec } from "@/lib/types";

type Result = { ok: true } | { ok: false; error: string };

function fail(e: unknown): Result {
  return { ok: false, error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e) };
}

export async function updateSettingsAction(p: {
  default_storage_type: string;
  default_storage_size: string;
  default_resources: ResourceSpec;
  default_pod_scheduling?: Record<string, unknown> | null;
  job_ttl_minutes: number;
}): Promise<Result> {
  try {
    await updateSettings(p);
    revalidatePath("/settings");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

/** id пустой — создать бакет, иначе обновить */
export async function saveS3BucketAction(id: string, p: S3BucketPayload): Promise<Result> {
  try {
    if (id) await updateS3Bucket(id, p);
    else await createS3Bucket(p);
    revalidatePath("/settings");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function deleteS3BucketAction(id: string): Promise<Result> {
  try {
    await deleteS3Bucket(id);
    revalidatePath("/settings");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function testS3BucketAction(id: string): Promise<Result> {
  try {
    await testS3Bucket(id);
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}
