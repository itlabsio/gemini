"use server";

import { revalidatePath } from "next/cache";
import { ApiError, createProject, deleteProject } from "@/lib/api";

type Result = { ok: true } | { ok: false; error: string };

function fail(e: unknown): Result {
  return { ok: false, error: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e) };
}

export async function createProjectAction(name: string): Promise<Result> {
  try {
    await createProject(name);
    revalidatePath("/projects");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}

export async function deleteProjectAction(name: string): Promise<Result> {
  try {
    await deleteProject(name);
    revalidatePath("/projects");
    return { ok: true };
  } catch (e) {
    return fail(e);
  }
}
