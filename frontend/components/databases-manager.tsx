"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { toast } from "sonner";
import { Pause, Play, RefreshCw, ScrollText } from "lucide-react";
import type { BackupRun, Database } from "@/lib/types";
import {
  activeRunsAction,
  discoverAction,
  patchDatabaseAction,
  runDatabaseAction,
} from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { StatusBadge } from "@/components/status-badge";
import { cn, fmtPgVersion } from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/ui/empty-state";

interface Props {
  instanceId: string;
  databases: Database[];
  initialActiveRuns: BackupRun[];
  defaultStorageSize: string;
  headRole: "source" | "target";
  canEdit: boolean;
  canRun: boolean;
}

type ActionResult = { ok: boolean; error?: string };

// Пока хотя бы у одной базы идёт прогон — опрашиваем чаще; в покое реже.
const POLL_ACTIVE_MS = 6000;
const POLL_IDLE_MS = 20000;

function indexByDb(runs: BackupRun[]): Map<string, BackupRun> {
  const m = new Map<string, BackupRun>();
  for (const r of runs) if (!m.has(r.database_id)) m.set(r.database_id, r);
  return m;
}

export function DatabasesManager({
  instanceId,
  databases,
  initialActiveRuns,
  defaultStorageSize,
  headRole,
  canEdit,
  canRun,
}: Props) {
  const router = useRouter();
  const [pending, start] = useTransition();

  // На target базы — цели восстановления, а не бэкапа.
  const isTarget = headRole === "target";
  const opDone = isTarget ? "Восстановление завершено" : "Бэкап завершён";
  const opDonePlural = isTarget ? "Восстановлений завершено" : "Бэкапов завершено";

  // Результат последнего опроса; null — опроса ещё не было с момента, как
  // пришли свежие серверные данные (тогда точка отсчёта — initialActiveRuns).
  const [polled, setPolled] = useState<Map<string, BackupRun> | null>(null);
  const [seenServer, setSeenServer] = useState(initialActiveRuns);
  if (seenServer !== initialActiveRuns) {
    setSeenServer(initialActiveRuns);
    setPolled(null);
  }

  const activeByDb = polled ?? indexByDb(initialActiveRuns);

  // Зеркало текущего состояния для сравнения в опросе — ref в эффекте, без setState.
  const activeRef = useRef(activeByDb);
  useEffect(() => {
    activeRef.current = activeByDb;
  }, [activeByDb]);

  const polling = useRef(false);
  const refreshActive = useCallback(async () => {
    if (polling.current) return;
    polling.current = true;
    let res;
    try {
      res = await activeRunsAction();
    } finally {
      polling.current = false;
    }
    if (!res.ok) return;
    const next = indexByDb(res.runs);
    const finished = [...activeRef.current.keys()].filter((id) => !next.has(id));
    setPolled(next);
    if (finished.length > 0) {
      toast.info(
        finished.length === 1
          ? `${opDone} — подробности в журнале`
          : `${opDonePlural}: ${finished.length}`,
      );
      router.refresh();
    }
  }, [router, opDone, opDonePlural]);

  const hasActive = activeByDb.size > 0;
  useEffect(() => {
    const iv = setInterval(
      refreshActive,
      hasActive ? POLL_ACTIVE_MS : POLL_IDLE_MS,
    );
    return () => clearInterval(iv);
  }, [refreshActive, hasActive]);

  const run = (fn: () => Promise<ActionResult>, okMsg?: string) => {
    start(async () => {
      const res = await fn();
      if (!res.ok) {
        toast.error(res.error ?? "Ошибка");
        return;
      }
      if (okMsg) toast.success(okMsg);
      // Прогон только что создан — сразу подхватываем его в таблицу.
      void refreshActive();
    });
  };

  const onDiscover = () =>
    start(async () => {
      const res = await discoverAction(instanceId);
      if (!res.ok) {
        toast.error(res.error ?? "Ошибка");
        return;
      }
      const v = fmtPgVersion(res.serverVersionNum);
      toast.success(v ? `Discovery выполнен · PostgreSQL ${v}` : "Discovery выполнен");
      router.refresh();
    });

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Базы данных</h2>
        {canEdit && (
          <Button
            variant="outline"
            size="sm"
            onClick={onDiscover}
            disabled={pending}
          >
            <RefreshCw className={pending ? "animate-spin" : undefined} />
            Discovery
          </Button>
        )}
      </div>

      {databases.length === 0 ? (
        <EmptyState
          title="Список пуст"
          description={
            canEdit
              ? "Запустите discovery, чтобы подтянуть базы с сервера"
              : "Базы появятся после discovery"
          }
        />
      ) : (
        <div className="rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>База</TableHead>
                <TableHead>{isTarget ? "Восстановление" : "Бэкап"}</TableHead>
                {headRole === "source" && <TableHead>Расписание (cron)</TableHead>}
                <TableHead>Хранилище дампа</TableHead>
                <TableHead className="text-center">Журнал</TableHead>
                <TableHead className="text-right">Статус / запуск</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {databases.map((db) => (
                <Row
                  key={db.id}
                  db={db}
                  instanceId={instanceId}
                  headRole={headRole}
                  canEdit={canEdit}
                  canRun={canRun}
                  disabled={pending}
                  activeRun={activeByDb.get(db.id)}
                  defaultStorageSize={defaultStorageSize}
                  run={run}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  );
}

function Row({
  db,
  instanceId,
  headRole,
  canEdit,
  canRun,
  disabled,
  activeRun,
  defaultStorageSize,
  run,
}: {
  db: Database;
  instanceId: string;
  headRole: "source" | "target";
  canEdit: boolean;
  canRun: boolean;
  disabled: boolean;
  activeRun?: BackupRun;
  defaultStorageSize: string;
  run: (fn: () => Promise<ActionResult>, okMsg?: string) => void;
}) {
  const [cron, setCron] = useState(db.schedule_cron ?? "");
  const [stType, setStType] = useState(db.storage_type ?? "");
  const [stSize, setStSize] = useState(db.storage_size ?? "");

  const cronDirty = cron !== (db.schedule_cron ?? "");
  const storageDirty =
    stType !== (db.storage_type ?? "") || stSize !== (db.storage_size ?? "");

  const running = Boolean(activeRun);

  // На Source у каждой базы есть CronJob; он suspended, если база выключена
  // или не задан schedule_cron — авто-бэкапа нет.
  const suspended =
    headRole === "source" &&
    db.present &&
    (!db.enabled || !(db.schedule_cron ?? "").trim());

  return (
    <TableRow className={cn(!db.present && "opacity-50")}>
      <TableCell>
        <div className="flex items-center gap-2">
          <span className="font-medium">{db.db_name}</span>
          {!db.present && (
            <Badge variant="outline" className="text-muted-foreground">
              исчезла
            </Badge>
          )}
        </div>
      </TableCell>

      <TableCell>
        <div className="flex items-center gap-2">
          <Switch
            checked={db.enabled}
            disabled={!canEdit || disabled || running}
            onCheckedChange={(checked) =>
              run(
                () => patchDatabaseAction(instanceId, db.id, { enabled: checked }),
                checked ? "База включена" : "База выключена",
              )
            }
          />
          <span className="text-sm text-muted-foreground">
            {db.enabled ? "включён" : "выключен"}
          </span>
        </div>
      </TableCell>

      {headRole === "source" && (
        <TableCell>
          <div className="flex items-center gap-2">
            <Input
              value={cron}
              onChange={(e) => setCron(e.target.value)}
              disabled={!canEdit || disabled}
              placeholder="0 2 * * *"
              className="h-8 w-32 font-mono text-xs"
            />
            {canEdit && cronDirty && (
              <Button
                size="sm"
                onClick={() =>
                  run(
                    () =>
                      patchDatabaseAction(instanceId, db.id, {
                        schedule_cron: cron,
                      }),
                    "Расписание сохранено",
                  )
                }
                disabled={disabled}
              >
                Применить
              </Button>
            )}
          </div>
        </TableCell>
      )}

      <TableCell>
        <div className="flex items-center gap-2">
          <Select
            value={stType || "default"}
            onValueChange={(v) => setStType(v === "default" ? "" : (v as "ephemeral" | "emptydir"))}
            disabled={!canEdit || disabled}
          >
            <SelectTrigger size="sm" className="w-[9.5rem]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="default">по умолчанию</SelectItem>
              <SelectItem value="ephemeral">ephemeral PVC</SelectItem>
              <SelectItem value="emptydir">emptyDir</SelectItem>
            </SelectContent>
          </Select>
          <Input
            value={stSize}
            onChange={(e) => setStSize(e.target.value)}
            disabled={!canEdit || disabled}
            placeholder={defaultStorageSize || "20Gi"}
            className="h-8 w-20 text-xs"
          />
          {canEdit && storageDirty && (
            <Button
              size="sm"
              onClick={() =>
                run(
                  () =>
                    patchDatabaseAction(instanceId, db.id, {
                      storage_type: stType,
                      storage_size: stSize,
                    }),
                  "Хранилище сохранено",
                )
              }
              disabled={disabled}
            >
              Применить
            </Button>
          )}
        </div>
      </TableCell>

      <TableCell className="text-center">
        <Tooltip>
          <TooltipTrigger asChild>
            <Link
              href={`/runs?database_id=${db.id}`}
              aria-label="Журнал запусков"
              className="inline-flex rounded-md p-1 text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <ScrollText className="size-4" />
            </Link>
          </TooltipTrigger>
          <TooltipContent>Журнал запусков</TooltipContent>
        </Tooltip>
      </TableCell>

      <TableCell className="text-right">
        <div className="flex justify-end">
          {running ? (
            <StatusBadge status={activeRun?.status} />
          ) : canRun && db.enabled ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                run(
                  () => runDatabaseAction(instanceId, db.id),
                  headRole === "source" ? "Dump запущен" : "Restore запущен",
                )
              }
              disabled={disabled}
            >
              <Play />
              Запустить
            </Button>
          ) : suspended ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="warning" className="gap-1">
                  <Pause />
                  suspend
                </Badge>
              </TooltipTrigger>
              <TooltipContent>
                CronJob в suspend — автоматический бэкап не идёт
              </TooltipContent>
            </Tooltip>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  );
}
