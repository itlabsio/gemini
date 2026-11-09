"use server";

import { revalidatePath } from "next/cache";
import {
  ApiError,
  createNotificationChannel,
  deleteNotificationChannel,
  deleteSystemBot,
  setSystemBot,
  testNotificationChannel,
  updateNotificationChannel,
  type NotificationChannelPayload,
} from "@/lib/api";
import type { NotificationProvider } from "@/lib/types";

type Result = { ok: true } | { ok: false; error: string };

function fail(e: unknown): Result {
  return { ok: false, error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e) };
}

/** project пустой — глобальные каналы (страница настроек). */
function revalidate(project: string) {
  revalidatePath(project ? `/projects/${encodeURIComponent(project)}/notifications` : "/settings");
}

/** id пустой — создать канал, иначе обновить */
export async function saveChannelAction(
  project: string,
  id: string,
  p: NotificationChannelPayload,
): Promise<Result> {
  try {
    if (id) await updateNotificationChannel(project, id, p);
    else await createNotificationChannel(project, p);
    revalidate(project);
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function deleteChannelAction(project: string, id: string): Promise<Result> {
  try {
    await deleteNotificationChannel(project, id);
    revalidate(project);
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function testChannelAction(project: string, id: string): Promise<Result> {
  try {
    await testNotificationChannel(project, id);
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function setSystemBotAction(provider: NotificationProvider, token: string): Promise<Result> {
  try {
    await setSystemBot(provider, token);
    revalidate("");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function deleteSystemBotAction(provider: NotificationProvider): Promise<Result> {
  try {
    await deleteSystemBot(provider);
    revalidate("");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}
