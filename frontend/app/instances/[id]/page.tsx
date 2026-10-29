import type { Metadata } from "next";
import type { ReactNode } from "react";
import Link from "next/link";
import { notFound } from "next/navigation";
import { ChevronRight } from "lucide-react";
import {
  ApiError,
  getInstance,
  getMe,
  getSettings,
  listActiveRuns,
  listDatabases,
  listRecentRuns,
} from "@/lib/api";
import { DatabasesManager } from "@/components/databases-manager";
import { InstanceActions } from "@/components/instance-actions";
import { SslModeBadge } from "@/components/ssl-badge";
import { SecretBadge } from "@/components/secret-badge";
import { ManagedBadge } from "@/components/managed-badge";
import { MiddleTruncate } from "@/components/middle-truncate";
import { fmtPgVersion, RUN_HISTORY_SLOTS } from "@/lib/utils";
import { can } from "@/lib/rbac";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";

interface Props {
  params: Promise<{ id: string }>;
}

function Detail({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium">{value}</dd>
    </div>
  );
}

/** Заголовок вкладки — имя инстанса; без доступа или при ошибке — общее «Инстанс». */
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { id } = await params;
  try {
    return { title: (await getInstance(id)).name };
  } catch {
    return { title: "Инстанс" };
  }
}

export default async function InstanceDetailPage({ params }: Props) {
  const { id } = await params;

  let instance;
  try {
    instance = await getInstance(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const me = await getMe();
  const [databases, activeRuns, recentRuns, settings] = await Promise.all([
    listDatabases(id).catch(() => []),
    listActiveRuns().catch(() => []),
    listRecentRuns(id, RUN_HISTORY_SLOTS).catch(() => []),
    getSettings().catch(() => null),
  ]);

  return (
    <div className="flex flex-col gap-6">
      <nav className="flex items-center gap-1 text-sm text-muted-foreground">
        <Link href="/instances" className="hover:text-foreground">
          Инстансы
        </Link>
        <ChevronRight className="size-3.5" />
        <span className="text-foreground">{instance.name}</span>
      </nav>

      <div className="flex items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">
            {instance.name}
          </h1>
          <Badge
            variant={instance.role === "target" ? "info" : "secondary"}
            className="capitalize"
          >
            {instance.role}
          </Badge>
          <ManagedBadge managedBy={instance.managed_by} />
        </div>
        {can(me, instance.project, "admin") && (
          <InstanceActions
            id={id}
            name={instance.name}
            canUnmanage={me.superuser && instance.managed_by === "terraform"}
          />
        )}
      </div>

      <Card>
        <CardContent>
          <dl className="flex flex-wrap gap-x-10 gap-y-5">
            <Detail
              label="Хост"
              value={
                <MiddleTruncate
                  text={`${instance.host}:${instance.port}`}
                  className="tabular-nums"
                />
              }
            />
            <Detail
              label="Проект"
              value={instance.project || <span className="text-muted-foreground">не назначен</span>}
            />
            <Detail label="SSL mode" value={<SslModeBadge mode={instance.ssl_mode} />} />
            {(instance.ssl_mode === "verify-ca" ||
              instance.ssl_mode === "verify-full") && (
              <Detail
                label="CA-сертификат"
                value={instance.ssl_root_cert ? "кастомный" : "встроенный"}
              />
            )}
            <Detail
              label="База discovery"
              value={instance.discovery_db || "postgres"}
            />
            {instance.role === "target" && (
              <Detail
                label="Стратегия restore"
                value={
                  instance.restore_mode === "in_place_per_db"
                    ? "in_place · пользователь на базу"
                    : (instance.restore_mode ?? "recreate")
                }
              />
            )}
            <Detail
              label="PostgreSQL"
              value={fmtPgVersion(instance.server_version_num) ?? "после discovery"}
            />
            {instance.excluded_databases &&
              instance.excluded_databases.length > 0 && (
                <Detail
                  label="Исключены из discovery"
                  value={
                    <span className="font-mono text-xs">
                      {instance.excluded_databases.join(", ")}
                    </span>
                  }
                />
              )}
            <Detail
              label="Секрет"
              value={
                <div className="flex min-w-0 flex-col gap-1">
                  <SecretBadge authType={instance.auth_type} />
                  {instance.auth_type === "vault" && instance.vault_path && (
                    <MiddleTruncate
                      text={instance.vault_path}
                      className="font-mono text-xs text-muted-foreground"
                    />
                  )}
                  {instance.auth_type === "plain" && instance.plain_username && (
                    <span className="truncate font-mono text-xs text-muted-foreground">
                      {instance.plain_username}
                    </span>
                  )}
                </div>
              }
            />
          </dl>
        </CardContent>
      </Card>

      <DatabasesManager
        instanceId={id}
        databases={databases}
        initialActiveRuns={activeRuns}
        recentRuns={recentRuns}
        defaultStorageSize={settings?.default_storage_size ?? ""}
        defaultStorageType={settings?.default_storage_type ?? ""}
        headRole={me.head_role}
        restoreMode={instance.restore_mode}
        canEdit={can(me, instance.project, "admin")}
        canRun={can(me, instance.project, "operator")}
      />
    </div>
  );
}
