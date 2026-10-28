import type { Me, ProjectRole } from "@/lib/types";

const RANK: Record<ProjectRole, number> = { viewer: 1, operator: 2, admin: 3 };

/**
 * Зеркало backend `auth.Identity.Can`: superuser может всё, остальные — только
 * с ролью не ниже min в проекте инстанса. Инстанс без проекта ("") — только superuser.
 * Для UI-guard'ов; окончательная проверка — на backend.
 */
export function can(me: Me, project: string | undefined, min: ProjectRole): boolean {
  if (me.superuser) return true;
  if (!project) return false;
  const role = me.projects?.[project];
  return !!role && RANK[role] >= RANK[min];
}

/** Проекты, где у пользователя роль не ниже min. */
export function projectsAtLeast(me: Me, min: ProjectRole): string[] {
  return Object.entries(me.projects ?? {})
    .filter(([, r]) => RANK[r] >= RANK[min])
    .map(([p]) => p)
    .sort();
}
