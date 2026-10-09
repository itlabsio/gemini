"use server";

import { revalidatePath } from "next/cache";
import {
  ApiError,
  listInstanceRoles,
  mappingCandidates,
  saveMapping,
  type MappingPayload,
} from "@/lib/api";
import type { Candidate } from "@/lib/types";

export async function loadCandidatesAction(): Promise<
  { ok: true; pairingId: string; candidates: Candidate[] } | { ok: false; error: string }
> {
  try {
    const res = await mappingCandidates();
    return { ok: true, pairingId: res.pairing_id, candidates: res.candidates };
  } catch (e) {
    return { ok: false, error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e) };
  }
}

export async function loadRolesAction(
  instanceId: string,
): Promise<{ ok: true; roles: string[] } | { ok: false; error: string }> {
  try {
    return { ok: true, roles: await listInstanceRoles(instanceId) };
  } catch (e) {
    return {
      ok: false,
      error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e),
    };
  }
}

export async function saveMappingAction(
  p: MappingPayload
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    await saveMapping(p);
    revalidatePath("/mapping");
    return { ok: true };
  } catch (e) {
    return { ok: false, error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e) };
  }
}
