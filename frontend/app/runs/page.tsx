import Link from "next/link";
import { X } from "lucide-react";
import { listRuns } from "@/lib/api";
import type { BackupRun } from "@/lib/types";
import { LocalDateTime } from "@/components/local-date-time";
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
  try {
    runs = await listRuns(qs.toString() ? `?${qs}` : "");
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
                <TableCell className="max-w-xs truncate font-mono text-xs">
                  {r.s3_object_key || "—"}
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
