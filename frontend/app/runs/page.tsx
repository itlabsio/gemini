import type { Metadata } from "next";
import Link from "next/link";
import { X } from "lucide-react";
import { getMe, listDatabases, listInstances, listRuns } from "@/lib/api";
import type { BackupRun, Me } from "@/lib/types";
import { can } from "@/lib/rbac";
import { fmtPgVersion } from "@/lib/utils";
import { LocalDateTime } from "@/components/local-date-time";
import { DumpsButton } from "@/components/dumps-dialog";
import { PageHeader } from "@/components/ui/page-header";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RunStatusCell } from "@/components/run-status-cell";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export const metadata: Metadata = { title: "Журнал" };

type DownloadableDb = { dbName: string; pgVersion: string | null };

/**
 * Базы, дампы которых пользователь может скачать (source, флаг инстанса, admin
 * проекта). Ошибка загрузки — просто без кнопок скачивания.
 */
async function downloadableDbs(me: Me): Promise<Map<string, DownloadableDb>> {
  const out = new Map<string, DownloadableDb>();
  if (me.head_role !== "source") return out;
  try {
    const instances = (await listInstances()).filter(
      (i) => i.downloadable && can(me, i.project, "admin"),
    );
    const lists = await Promise.all(instances.map((i) => listDatabases(i.id)));
    instances.forEach((inst, idx) => {
      for (const db of lists[idx]) {
        out.set(db.id, { dbName: db.db_name, pgVersion: fmtPgVersion(inst.server_version_num) });
      }
    });
  } catch {
    // кнопки скачивания не критичны для журнала
  }
  return out;
}

export default async function RunsPage({
  searchParams,
}: {
  searchParams: Promise<{ status?: string; kind?: string; database_id?: string }>;
}) {
  const sp = await searchParams;
  const qs = new URLSearchParams();
  if (sp.status) qs.set("status", sp.status);
  if (sp.kind) qs.set("kind", sp.kind);
  if (sp.database_id) qs.set("database_id", sp.database_id);

  let runs: BackupRun[] = [];
  let error: string | null = null;
  let dumpDbs = new Map<string, DownloadableDb>();
  try {
    const [list, me] = await Promise.all([listRuns(qs.toString() ? `?${qs}` : ""), getMe()]);
    runs = list;
    dumpDbs = await downloadableDbs(me);
  } catch (e) {
    error = e instanceof Error ? e.message : "Ошибка загрузки";
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Журнал запусков"
        description="История бэкапов и восстановлений"
        actions={
          sp.database_id && (
            <Button asChild variant="outline" size="sm">
              <Link href="/runs">
                <X />
                Сбросить фильтр по базе
              </Link>
            </Button>
          )
        }
      />

      {error && <Alert variant="destructive">{error}</Alert>}

      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Тип</TableHead>
              <TableHead>Статус</TableHead>
              <TableHead>Триггер</TableHead>
              <TableHead>Начат</TableHead>
              <TableHead>Завершён</TableHead>
              <TableHead>Объект S3</TableHead>
              <TableHead>Target</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {runs.map((r) => (
              <TableRow key={r.id}>
                <TableCell>
                  <Badge variant="secondary" className="capitalize">
                    {r.kind}
                  </Badge>
                </TableCell>
                <TableCell>
                  <RunStatusCell status={r.status} error={r.error_message} />
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {r.trigger}
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  <LocalDateTime value={r.started_at} />
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  <LocalDateTime value={r.finished_at} />
                </TableCell>
                <TableCell className="max-w-xs font-mono text-xs">
                  <div className="flex items-center gap-1">
                    <span className="truncate">{r.s3_object_key || "—"}</span>
                    {r.kind === "dump" && r.status === "succeeded" && r.s3_object_key &&
                      dumpDbs.has(r.database_id) && (
                        <DumpsButton
                          databaseId={r.database_id}
                          dbName={dumpDbs.get(r.database_id)!.dbName}
                          pgVersion={dumpDbs.get(r.database_id)!.pgVersion}
                          focusKey={r.s3_object_key}
                        />
                      )}
                  </div>
                </TableCell>
                <TableCell>
                  {r.peer_statuses && r.peer_statuses.length > 0 ? (
                    <div className="flex flex-col gap-1">
                      {r.peer_statuses.map((ps) => (
                        <div
                          key={ps.peer_url}
                          className="flex items-center gap-2"
                        >
                          <RunStatusCell
                            status={ps.status}
                            error={ps.error}
                            title="Ошибка на стороне Target"
                          />
                          <span
                            className="max-w-[16rem] truncate font-mono text-xs text-muted-foreground"
                            title={ps.peer_url}
                          >
                            {ps.peer_url}
                          </span>
                        </div>
                      ))}
                    </div>
                  ) : (
                    "—"
                  )}
                </TableCell>
              </TableRow>
            ))}
            {runs.length === 0 && !error && (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className="py-10 text-center text-muted-foreground"
                >
                  Запусков пока нет
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
