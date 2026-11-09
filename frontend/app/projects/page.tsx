import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { getMe, listProjects } from "@/lib/api";
import { projectsAtLeast } from "@/lib/rbac";
import type { Project } from "@/lib/types";
import { ProjectsManager } from "@/components/projects-manager";
import { PageHeader } from "@/components/ui/page-header";
import { Alert } from "@/components/ui/alert";

export const metadata: Metadata = { title: "Проекты" };

export default async function ProjectsPage() {
  const me = await getMe();
  if (!me.superuser) {
    const names = projectsAtLeast(me, "admin");
    if (names.length === 0) redirect("/instances");
    return (
      <div className="mx-auto flex max-w-2xl flex-col gap-6">
        <PageHeader title="Проекты" description="Проекты, где у вас роль admin" />
        <ul className="flex flex-col gap-2">
          {names.map((n) => (
            <li key={n}>
              <Link href={`/projects/${encodeURIComponent(n)}/notifications`} className="hover:underline">
                {n} — уведомления
              </Link>
            </li>
          ))}
        </ul>
      </div>
    );
  }

  let projects: Project[] = [];
  let error: string | null = null;
  try {
    projects = await listProjects();
  } catch (e) {
    error = e instanceof Error ? e.message : "Ошибка загрузки";
  }

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader
        title="Проекты"
        description="Проект — имя группы Keycloak верхнего уровня (/<проект>/<роль>). Инстансом управляют только участники его проекта; роли: admin, developer (operator), readonly (viewer)"
      />
      {error && <Alert variant="destructive">{error}</Alert>}
      <ProjectsManager projects={projects} />
    </div>
  );
}
