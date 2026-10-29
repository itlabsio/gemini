"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { toast } from "sonner";
import { ChevronRight, Pause, Play, RefreshCw, ScrollText } from "lucide-react";
import type { BackupRun, Database, RestoreMode } from "@/lib/types";
import {
  activeRunsAction,
  cancelRunAction,
  deleteDatabaseAction,
  discoverAction,
  loadLoginRolesAction,
  patchDatabaseAction,
  runDatabaseAction,
} from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { StatusBadge } from "@/components/status-badge";
import { RunningStopButton } from "@/components/running-stop-button";
import { DeleteDatabaseButton } from "@/components/delete-database-button";
import { RunHistoryStrip } from "@/components/run-history-strip";
import {
  bytesQuantityError,
  cn,
  fmtPgVersion,
  MIN_STORAGE_SIZE,
  RUN_HISTORY_SLOTS,
} from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
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
  /** Последние прогоны баз инстанса, от новых к старым. */
  recentRuns: BackupRun[];
  defaultStorageSize: string;
  /** Тип тома из настроек ("" — не загрузились). */
  defaultStorageType: string;
  headRole: "source" | "target";
  /** Стратегия restore инстанса (только target). */
  restoreMode?: RestoreMode;
  canEdit: boolean;
  canRun: boolean;
}

/** Роли с LOGIN для выбора пользователя restore: null — ещё грузятся. */
const STORAGE_LABEL: Record<string, string> = { ephemeral: "ephemeral PVC", emptydir: "emptyDir" };

type LoginRoles = string[] | "error" | null;

type ActionResult = { ok: boolean; error?: string };

// Пока хотя бы у одной базы идёт прогон — опрашиваем чаще; в покое реже.
const POLL_ACTIVE_MS = 6000;
const POLL_IDLE_MS = 20000;

function indexByDb(runs: BackupRun[]): Map<string, BackupRun> {
  const m = new Map<string, BackupRun>();
  for (const r of runs) if (!m.has(r.database_id)) m.set(r.database_id, r);
  return m;
}

function groupByDb(runs: BackupRun[]): Map<string, BackupRun[]> {
  const m = new Map<string, BackupRun[]>();
  for (const r of runs) {
    const list = m.get(r.database_id);
    if (list) list.push(r);
    else m.set(r.database_id, [r]);
  }
  return m;
}

const NO_RUNS: BackupRun[] = [];

// Один размер для всех состояний колонки «Статус / запуск» — бейджи, кнопка
// «Запустить» и running/«Остановить» не дёргают ширину таблицы при смене.
const STATUS_SLOT = "h-7 w-32 justify-center";

// Подсказка к серым (унаследованным) значениям хранилища.
const INHERITED_HINT = "Из Настроек — меняется там для всех баз";

export function DatabasesManager({
  instanceId,
  databases,
  initialActiveRuns,
  recentRuns,
  defaultStorageSize,
  defaultStorageType,
  headRole,
  restoreMode,
  canEdit,
  canRun,
}: Props) {
  const router = useRouter();
  const [pending, start] = useTransition();

  // На target базы — цели восстановления, а не бэкапа.
  const isTarget = headRole === "target";
  // in_place_per_db: у каждой базы свой пользователь restore (обязателен).
  const perDB = isTarget && restoreMode === "in_place_per_db";

  const [loginRoles, setLoginRoles] = useState<LoginRoles>(null);
  useEffect(() => {
    if (!perDB || !canEdit) return;
    let alive = true;
    loadLoginRolesAction(instanceId).then((res) => {
      if (alive) setLoginRoles(res.ok ? res.roles : "error");
    });
    return () => {
      alive = false;
    };
  }, [perDB, canEdit, instanceId]);
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

  // Удалённые с сервера базы хранятся ради журнала и настроек на случай
  // возврата — показываем их свёрнутым блоком под основными.
  const presentDbs = databases.filter((d) => d.present);
  const deletedDbs = databases.filter((d) => !d.present);
  const [showDeleted, setShowDeleted] = useState(false);
  const colCount = 5 + (headRole === "source" ? 1 : 0) + (perDB ? 1 : 0);

  const activeByDb = polled ?? indexByDb(initialActiveRuns);
  const recentByDb = groupByDb(recentRuns);

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

  // Остановка прогона: базу сразу убираем из «активных», чтобы опрос не принял
  // остановку за завершение и не показал тост «Бэкап завершён».
  const stopRun = (r: BackupRun) =>
    start(async () => {
      const res = await cancelRunAction(instanceId, r.id);
      if (!res.ok) {
        toast.error(res.error ?? "Ошибка");
        return;
      }
      toast.success("Прогон остановлен");
      const next = new Map(activeRef.current);
      next.delete(r.database_id);
      activeRef.current = next;
      setPolled(next);
      void refreshActive();
      // Опрос остановку завершением не считает — историю обновляем сами.
      router.refresh();
    });

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

  const renderRow = (db: Database) => (
    <Row
      key={db.id}
      db={db}
      instanceId={instanceId}
      headRole={headRole}
      perDB={perDB}
      loginRoles={loginRoles}
      canEdit={canEdit}
      canRun={canRun}
      disabled={pending}
      activeRun={activeByDb.get(db.id)}
      recentRuns={recentByDb.get(db.id) ?? NO_RUNS}
      defaultStorageSize={defaultStorageSize}
      defaultStorageType={defaultStorageType}
      run={run}
      onStop={stopRun}
    />
  );

  return (
    <section
      className={cn(
        "flex flex-col gap-3",
        // Колонка пользователя restore не влезает в max-w-6xl — выходим за контейнер
        // симметрично, но не шире вьюпорта.
        perDB && "w-[min(88rem,calc(100vw-2rem))] self-center",
      )}
    >
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
                {perDB && <TableHead>Пользователь restore</TableHead>}
                <TableHead>Хранилище дампа</TableHead>
                <TableHead>Последние запуски</TableHead>
                <TableHead className="text-right">Статус / запуск</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {presentDbs.map(renderRow)}
              {deletedDbs.length > 0 && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={colCount} className="py-1.5">
                    <button
                      type="button"
                      onClick={() => setShowDeleted((v) => !v)}
                      aria-expanded={showDeleted}
                      className="inline-flex items-center gap-1 rounded-md px-1 text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                    >
                      <ChevronRight
                        className={cn("size-4 transition-transform", showDeleted && "rotate-90")}
                      />
                      Удалённые ({deletedDbs.length})
                    </button>
                  </TableCell>
                </TableRow>
              )}
              {showDeleted && deletedDbs.map(renderRow)}
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
  perDB,
  loginRoles,
  canEdit,
  canRun,
  disabled,
  activeRun,
  recentRuns,
  defaultStorageSize,
  defaultStorageType,
  run,
  onStop,
}: {
  db: Database;
  instanceId: string;
  headRole: "source" | "target";
  perDB: boolean;
  loginRoles: LoginRoles;
  canEdit: boolean;
  canRun: boolean;
  disabled: boolean;
  activeRun?: BackupRun;
  recentRuns: BackupRun[];
  defaultStorageSize: string;
  /** Тип тома из настроек ("" — не загрузились). */
  defaultStorageType: string;
  run: (fn: () => Promise<ActionResult>, okMsg?: string) => void;
  onStop: (r: BackupRun) => void;
}) {
  const [cron, setCron] = useState(db.schedule_cron ?? "");
  const [stType, setStType] = useState(db.storage_type ?? "");
  const [stSize, setStSize] = useState(db.storage_size ?? "");

  const cronDirty = cron !== (db.schedule_cron ?? "");
  const storageDirty =
    stType !== (db.storage_type ?? "") || stSize !== (db.storage_size ?? "");

  const running = Boolean(activeRun);
  // Активный прогон из опроса свежее серверной истории: подставляем его, чтобы
  // только что запущенный сразу появился в полоске.
  const history = activeRun
    ? [activeRun, ...recentRuns.filter((r) => r.id !== activeRun.id)]
    : recentRuns;
  // Без своего пользователя restore в in_place_per_db не запустится — бэкенд
  // откажет, поэтому и кнопку не показываем.
  const missingRestoreUser = perDB && !db.restore_username;
  const sizeErr = bytesQuantityError(stSize, { min: MIN_STORAGE_SIZE });

  // На Source у каждой базы есть CronJob; он suspended, если база выключена
  // или не задан schedule_cron — авто-бэкапа нет.
  const noCron = !(db.schedule_cron ?? "").trim();
  const suspended = headRole === "source" && db.present && (!db.enabled || noCron);
  // Тумблер только разрешает запуск; без крона бэкап идёт лишь по кнопке.
  const manualOnly = headRole === "source" && db.enabled && noCron;

  return (
    // Удалённую базу приглушаем целиком, кроме кнопки «Удалить» в последней колонке.
    <TableRow className={cn(!db.present && "[&>td:not(:last-child)]:opacity-50")}>
      <TableCell>
        <div className="flex items-center gap-2">
          <span className="font-medium">{db.db_name}</span>
          {!db.present && (
            <Badge variant="outline" className="text-muted-foreground">
              удалена
            </Badge>
          )}
        </div>
      </TableCell>

      <TableCell>
        <div className="flex items-center gap-3">
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
          {headRole !== "source" ? (
            <span className="text-sm text-muted-foreground">
              {db.enabled ? "включён" : "выключен"}
            </span>
          ) : !db.enabled ? null : manualOnly ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="cursor-help text-sm text-warning">вручную</span>
              </TooltipTrigger>
              <TooltipContent>
                Расписание не задано — автоматического бэкапа нет, только ручной
                запуск
              </TooltipContent>
            </Tooltip>
          ) : (
            <span className="text-sm text-muted-foreground">по расписанию</span>
          )}
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

      {perDB && (
        <TableCell>
          <RestoreUserCell
            db={db}
            instanceId={instanceId}
            loginRoles={loginRoles}
            canEdit={canEdit}
            disabled={disabled}
            run={run}
          />
        </TableCell>
      )}

      <TableCell>
        <div className="flex items-center gap-2">
          <Select
            value={stType || "default"}
            onValueChange={(v) => setStType(v === "default" ? "" : (v as "ephemeral" | "emptydir"))}
            disabled={!canEdit || disabled}
          >
            <SelectTrigger
              size="sm"
              title={stType ? undefined : INHERITED_HINT}
              className={cn("w-36", !stType && "text-muted-foreground")}
            >
              {/* В триггере — само значение; «наследуется» передаёт серый цвет. */}
              <SelectValue>
                {STORAGE_LABEL[stType || defaultStorageType] ?? (stType || defaultStorageType || "—")}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="default">
                Как в настройках
                {defaultStorageType && ` · ${STORAGE_LABEL[defaultStorageType] ?? defaultStorageType}`}
              </SelectItem>
              <SelectSeparator />
              <SelectItem value="ephemeral">ephemeral PVC</SelectItem>
              <SelectItem value="emptydir">emptyDir</SelectItem>
            </SelectContent>
          </Select>
          <div className="relative">
            <Input
              value={stSize}
              onChange={(e) => setStSize(e.target.value)}
              disabled={!canEdit || disabled}
              placeholder={defaultStorageSize || "—"}
              aria-invalid={Boolean(sizeErr)}
              title={sizeErr ?? (stSize ? undefined : INHERITED_HINT)}
              className="h-8 w-20 text-xs"
            />
            {sizeErr && (
              <p className="absolute top-full left-0 mt-0.5 text-[11px] whitespace-nowrap text-destructive">
                {sizeErr}
              </p>
            )}
          </div>
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
              disabled={disabled || Boolean(sizeErr)}
            >
              Применить
            </Button>
          )}
        </div>
      </TableCell>

      <TableCell>
        <div className="flex items-center gap-2">
          <RunHistoryStrip
            runs={history}
            slots={RUN_HISTORY_SLOTS}
            journalHref={`/runs?database_id=${db.id}`}
          />
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
        </div>
      </TableCell>

      <TableCell className="text-right">
        <div className="flex justify-end">
          {activeRun && canRun ? (
            <RunningStopButton
              status={activeRun.status}
              kind={activeRun.kind}
              dbName={db.db_name}
              disabled={disabled}
              onStop={() => onStop(activeRun)}
              className={STATUS_SLOT}
            />
          ) : running ? (
            <StatusBadge status={activeRun?.status} className={STATUS_SLOT} />
          ) : !db.present ? (
            canEdit && (
              <DeleteDatabaseButton
                dbName={db.db_name}
                headRole={headRole}
                disabled={disabled}
                onDelete={() =>
                  run(() => deleteDatabaseAction(instanceId, db.id), "База удалена из Gemini")
                }
                className={STATUS_SLOT}
              />
            )
          ) : canRun && db.enabled && missingRestoreUser ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="warning" className={STATUS_SLOT}>
                  нет пользователя
                </Badge>
              </TooltipTrigger>
              <TooltipContent>
                Инстанс в режиме «пользователь на базу» — задайте пользователя restore
              </TooltipContent>
            </Tooltip>
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
              className={STATUS_SLOT}
            >
              <Play />
              Запустить
            </Button>
          ) : suspended ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="warning" className={cn("gap-1", STATUS_SLOT)}>
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

/**
 * Пользователь restore базы (in_place_per_db): роль с LOGIN из списка кластера
 * (или вручную, если список не прочитался) и пароль. Пароль бэкенд не отдаёт:
 * пустое поле при сохранённом пользователе — «не менять».
 */
function RestoreUserCell({
  db,
  instanceId,
  loginRoles,
  canEdit,
  disabled,
  run,
}: {
  db: Database;
  instanceId: string;
  loginRoles: LoginRoles;
  canEdit: boolean;
  disabled: boolean;
  run: (fn: () => Promise<ActionResult>, okMsg?: string) => void;
}) {
  const saved = db.restore_username ?? "";
  const [user, setUser] = useState(saved);
  const [password, setPassword] = useState("");

  const userChanged = user.trim() !== saved;
  const dirty = userChanged || password !== "";
  // Новому пользователю (или первой настройке) пароль нужен обязательно.
  const needPassword = user.trim() !== "" && (saved === "" || userChanged) && password === "";

  if (!canEdit) {
    return saved ? (
      <span className="font-mono text-xs">{saved}</span>
    ) : (
      <Badge variant="warning">не задан</Badge>
    );
  }

  return (
    <div className="flex items-center gap-2">
      {Array.isArray(loginRoles) ? (
        <Select value={user} onValueChange={setUser} disabled={disabled}>
          <SelectTrigger size="sm" className="w-40" aria-invalid={!user}>
            <SelectValue placeholder="— роль —" />
          </SelectTrigger>
          <SelectContent>
            {/* Сохранённый пользователь мог пропасть из кластера — не теряем его в списке. */}
            {(saved && !loginRoles.includes(saved) ? [saved, ...loginRoles] : loginRoles).map(
              (r) => (
                <SelectItem key={r} value={r}>
                  {r}
                </SelectItem>
              ),
            )}
          </SelectContent>
        </Select>
      ) : (
        <Input
          value={user}
          onChange={(e) => setUser(e.target.value)}
          disabled={disabled}
          placeholder={loginRoles === null ? "загрузка ролей…" : "имя роли"}
          aria-invalid={!user}
          className="h-8 w-40 font-mono text-xs"
        />
      )}
      <Input
        type="password"
        autoComplete="new-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        disabled={disabled}
        placeholder={saved && !userChanged ? "без изменений" : "пароль"}
        aria-invalid={needPassword}
        className="h-8 w-32 text-xs"
      />
      {dirty && (
        <Button
          size="sm"
          onClick={() =>
            run(async () => {
              const res = await patchDatabaseAction(instanceId, db.id, {
                restore_username: user.trim(),
                restore_password: password,
              });
              if (res.ok) setPassword("");
              return res;
            }, "Пользователь restore сохранён")
          }
          disabled={disabled || !user.trim() || needPassword}
        >
          Применить
        </Button>
      )}
    </div>
  );
}
