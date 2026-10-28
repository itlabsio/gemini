import type { Metadata } from "next";
import Link from "next/link";
import { Database, Plus } from "lucide-react";
import { listInstances, getMe } from "@/lib/api";
import type { Instance } from "@/lib/types";
import { PageHeader } from "@/components/ui/page-header";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { EmptyState } from "@/components/ui/empty-state";
import { InstancesTable } from "@/components/instances-table";
import { InstancesProjectFilter } from "@/components/instances-project-filter";

export const metadata: Metadata = { title: "Инстансы" };

export default async function InstancesPage({
  searchParams,
}: {
  searchParams: Promise<{ project?: string; unassigned?: string }>;
}) {
  const sp = await searchParams;
  const unassigned = sp.unassigned === "1";
  let instances: Instance[] = [];
  let error: string | null = null;
  let canEdit = false;

  try {
    const [list, me] = await Promise.all([listInstances(), getMe()]);
    instances = list;
    // создавать инстансы и назначать проект может только superuser
    canEdit = me.superuser;
  } catch (e) {
    error = e instanceof Error ? e.message : "Ошибка загрузки";
  }

  // Фильтр — по уже видимым пользователю инстансам (RBAC режет список на backend).
  // Проект из URL оставляем в списке, даже если его инстансов не видно, — иначе
  // Select показал бы пустое значение.
  const projects = [
    ...new Set([...instances.map((i) => i.project), sp.project ?? ""].filter(Boolean)),
  ].sort();
  const hasUnassigned = instances.some((i) => !i.project);
  const filtered = unassigned || !!sp.project;
  const shown = unassigned
    ? instances.filter((i) => !i.project)
    : sp.project
      ? instances.filter((i) => i.project === sp.project)
      : instances;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Инстансы"
        description="Серверы PostgreSQL и их базы. Мастер создания: хост/порт/секрет → discovery → выбор баз"
        actions={
          canEdit && (
            <Button asChild>
              <Link href="/instances/new">
                <Plus />
                Новый инстанс
              </Link>
            </Button>
          )
        }
      />

      {error && <Alert variant="destructive">{error}</Alert>}

      {projects.length + (hasUnassigned ? 1 : 0) > 1 || filtered ? (
        <InstancesProjectFilter
          projects={projects}
          value={sp.project}
          unassigned={unassigned}
          showUnassigned={hasUnassigned}
        />
      ) : null}

      {!error && filtered && shown.length === 0 && (
        <EmptyState
          icon={Database}
          title="Нет инстансов"
          description={
            unassigned
              ? "Все инстансы назначены проектам"
              : `В проекте «${sp.project}» нет доступных вам инстансов`
          }
        />
      )}

      {!error && !filtered && instances.length === 0 && (
        <EmptyState
          icon={Database}
          title="Инстансов пока нет"
          description="Добавьте первый сервер PostgreSQL, чтобы начать"
          action={
            canEdit && (
              <Button asChild size="sm">
                <Link href="/instances/new">
                  <Plus />
                  Новый инстанс
                </Link>
              </Button>
            )
          }
        />
      )}

      {shown.length > 0 && <InstancesTable instances={shown} />}
    </div>
  );
}
