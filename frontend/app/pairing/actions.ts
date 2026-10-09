"use server";

import { revalidatePath } from "next/cache";
import {
  ApiError,
  deletePairing,
  initPairing,
  issuePairingCode,
  revokePairing,
} from "@/lib/api";

export async function issuePairingCodeAction(): Promise<
  | { ok: true; code: string; expiresIn: number }
  | { ok: false; error: string; retryAfter?: number }
> {
  try {
    const res = await issuePairingCode();
    revalidatePath("/pairing");
    return { ok: true, code: res.code, expiresIn: res.expires_in_seconds };
  } catch (e) {
    if (e instanceof ApiError) {
      let retryAfter: number | undefined;
      if (e.body && typeof e.body === "object" && "retry_after_seconds" in e.body) {
        const n = Number((e.body as Record<string, unknown>).retry_after_seconds);
        if (Number.isFinite(n) && n > 0) retryAfter = n;
      }
      return { ok: false, error: `${e.status}: ${e.message}`, retryAfter };
    }
    return { ok: false, error: String(e) };
  }
}

export async function deletePairingAction(
  id: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    await deletePairing(id);
    revalidatePath("/pairing");
    return { ok: true };
  } catch (e) {
    return {
      ok: false,
      error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e),
    };
  }
}

export async function revokePairingAction(
  id: string,
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    await revokePairing(id);
    revalidatePath("/pairing");
    return { ok: true };
  } catch (e) {
    return {
      ok: false,
      error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e),
    };
  }
}

export async function initPairingAction(
  sourceURL: string,
  code: string
): Promise<{ ok: true } | { ok: false; error: string }> {
  try {
    await initPairing(sourceURL, code);
    revalidatePath("/pairing");
    return { ok: true };
  } catch (e) {
    return { ok: false, error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e) };
  }
}
