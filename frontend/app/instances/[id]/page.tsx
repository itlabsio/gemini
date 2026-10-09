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
} from "@/lib/api";
import { DatabasesManager } from "@/components/databases-manager";
import { InstanceActions } from "@/components/instance-actions";
import { SslModeBadge } from "@/components/ssl-badge";
import { SecretBadge } from "@/components/secret-badge";
import { MiddleTruncate } from "@/components/middle-truncate";
import { fmtPgVersion } from "@/lib/utils";
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

export default async function InstanceDetailPage({ params }: Props) {
  const { id } = await params;

  let instance;
  try {
    instance = await getInstance(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const [databases, me, activeRuns, settings] = await Promise.all([
    listDatabases(id).catch(() => []),
    getMe(),
    listActiveRuns().catch(() => []),
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
        </div>
        {me.role === "admin" && (
          <InstanceActions id={id} name={instance.name} />
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
                  className="font-mono text-xs"
                />
              }
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
        defaultStorageSize={settings?.default_storage_size ?? ""}
        headRole={me.head_role}
        canEdit={me.role === "admin"}
        canRun={me.role === "admin" || me.role === "operator"}
      />
    </div>
  );
}
