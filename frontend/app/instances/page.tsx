import Link from "next/link";
import { Database, Plus } from "lucide-react";
import { listInstances, getMe } from "@/lib/api";
import type { Instance } from "@/lib/types";
import { PageHeader } from "@/components/ui/page-header";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { EmptyState } from "@/components/ui/empty-state";
import { InstancesTable } from "@/components/instances-table";

export default async function InstancesPage() {
  let instances: Instance[] = [];
  let error: string | null = null;
  let canEdit = false;

  try {
    const [list, me] = await Promise.all([listInstances(), getMe()]);
    instances = list;
    canEdit = me.role === "admin";
  } catch (e) {
    error = e instanceof Error ? e.message : "Ошибка загрузки";
  }

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

      {!error && instances.length === 0 && (
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

      {instances.length > 0 && <InstancesTable instances={instances} />}
    </div>
  );
}
