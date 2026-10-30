"use client";

import { useRef, useState, useTransition } from "react";
import { toast } from "sonner";
import { Copy, Download, Loader2 } from "lucide-react";
import type { Dump, DumpDownload } from "@/lib/types";
import { downloadDumpAction, listDumpsAction } from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { LocalDateTime } from "@/components/local-date-time";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

const UNITS = ["B", "KiB", "MiB", "GiB", "TiB"];

function fmtBytes(n: number): string {
  let v = n;
  let i = 0;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${UNITS[i]}`;
}

// Выданную ссылку переиспользуем, пока до её истечения больше этого запаса:
// повторное «Скачать» не идёт на сервер (новая ссылка, запись в аудит, лимит).
const LINK_REUSE_MARGIN_MS = 60_000;

/** Имя файла дампа без префикса <инстанс>/<база>/. */
const baseName = (key: string) => key.slice(key.lastIndexOf("/") + 1);

interface Props {
  databaseId: string;
  dbName: string;
  /** Версия PostgreSQL инстанса («17.4») — для подсказки про pg_restore. */
  pgVersion?: string | null;
  /** Ключ дампа, который подсветить (переход из журнала запусков). */
  focusKey?: string;
}

/**
 * Кнопка-иконка «Скачать дамп» и диалог со списком готовых дампов базы в S3:
 * пользователь выбирает бакет (если копий несколько) и получает presigned-ссылку.
 */
export function DumpsButton({ databaseId, dbName, pgVersion, focusKey }: Props) {
  const [open, setOpen] = useState(false);
  const [dumps, setDumps] = useState<Dump[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, startLoad] = useTransition();

  const load = () =>
    startLoad(async () => {
      setError(null);
      const res = await listDumpsAction(databaseId);
      if (res.ok) setDumps(res.dumps);
      else setError(res.error);
    });

  const onOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) load();
  };

  const focusMissing = focusKey && dumps && !dumps.some((d) => d.key === focusKey);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={() => onOpenChange(true)}
            aria-label="Скачать дамп"
            className="inline-flex rounded-md p-1 text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <Download className="size-4" />
          </button>
        </TooltipTrigger>
        <TooltipContent>Скачать дамп</TooltipContent>
      </Tooltip>

      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Дампы базы {dbName}</DialogTitle>
          <DialogDescription>
            Ссылка временная, работает у любого, у кого она есть. Каждая выдача
            пишется в журнал аудита.
          </DialogDescription>
        </DialogHeader>

        {error && <Alert variant="destructive">{error}</Alert>}
        {focusMissing && (
          <Alert>Дампа этого запуска ({baseName(focusKey)}) в S3 больше нет.</Alert>
        )}

        {loading && !dumps ? (
          <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Загрузка списка дампов…
          </div>
        ) : dumps && dumps.length === 0 ? (
          <p className="py-6 text-sm text-muted-foreground">Готовых дампов в S3 нет.</p>
        ) : (
          dumps && (
            <ul className="flex max-h-[45vh] flex-col divide-y overflow-auto rounded-md border">
              {dumps.map((d) => (
                <DumpRow
                  key={d.key}
                  dump={d}
                  databaseId={databaseId}
                  pgVersion={pgVersion}
                  focused={d.key === focusKey}
                />
              ))}
            </ul>
          )
        )}
      </DialogContent>
    </Dialog>
  );
}

function DumpRow({
  dump,
  databaseId,
  pgVersion,
  focused,
}: {
  dump: Dump;
  databaseId: string;
  pgVersion?: string | null;
  focused: boolean;
}) {
  const [bucketId, setBucketId] = useState(dump.buckets[0]?.id ?? "");
  // Ссылки по бакетам: у каждого бакета своя.
  const [links, setLinks] = useState<Record<string, DumpDownload>>({});
  const result = links[bucketId] ?? null;
  const [pending, start] = useTransition();
  // Двойной клик успевает до перерисовки с disabled — держим флаг синхронно.
  const inFlight = useRef(false);

  const onDownload = () => {
    if (inFlight.current) return;
    // S3 отдаёт файл с Content-Disposition: attachment — страница остаётся.
    if (result && new Date(result.expires_at).getTime() - Date.now() > LINK_REUSE_MARGIN_MS) {
      window.location.assign(result.url);
      return;
    }
    inFlight.current = true;
    start(async () => {
      try {
        const res = await downloadDumpAction(databaseId, dump.key, bucketId);
        if (!res.ok) {
          toast.error(res.error);
          return;
        }
        setLinks((l) => ({ ...l, [bucketId]: res.download }));
        window.location.assign(res.download.url);
      } finally {
        inFlight.current = false;
      }
    });
  };

  const copyLink = () => {
    if (!result) return;
    navigator.clipboard.writeText(result.url).then(() => toast.success("Ссылка скопирована"));
  };

  return (
    <li className={cn("flex flex-col gap-2 p-3", focused && "bg-accent/50")}>
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate font-mono text-xs" title={dump.key}>
            {baseName(dump.key)}
          </span>
          <span className="text-xs text-muted-foreground">
            <LocalDateTime value={dump.last_modified} /> · {fmtBytes(dump.size)}
          </span>
        </div>
        {dump.buckets.length === 0 ? (
          <span className="text-xs text-muted-foreground">
            только в бакетах без скачивания
          </span>
        ) : (
          <>
            {dump.buckets.length > 1 ? (
              <Select value={bucketId} onValueChange={setBucketId} disabled={pending}>
                <SelectTrigger size="sm" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {dump.buckets.map((b) => (
                    <SelectItem key={b.id} value={b.id}>
                      {b.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <span className="text-xs text-muted-foreground">{dump.buckets[0].name}</span>
            )}
            <Button size="sm" onClick={onDownload} disabled={pending || !bucketId}>
              {pending ? <Loader2 className="animate-spin" /> : <Download />}
              Скачать
            </Button>
          </>
        )}
      </div>

      {result && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 rounded-md bg-muted/40 p-2 text-xs">
          <dt className="text-muted-foreground">sha256</dt>
          <dd className="font-mono break-all">{result.sha256 || "—"}</dd>
          <dt className="text-muted-foreground">Расширения</dt>
          <dd className="font-mono">
            {result.extensions.length ? result.extensions.join(", ") : "нет"}
          </dd>
          <dt className="text-muted-foreground">Ссылка до</dt>
          <dd className="flex items-center gap-2">
            <LocalDateTime value={result.expires_at} />
            <Button variant="ghost" size="sm" className="h-6 px-2" onClick={copyLink}>
              <Copy />
              Копировать ссылку
            </Button>
          </dd>
          <dd className="col-span-2 text-muted-foreground">
            Формат custom: восстанавливать через{" "}
            <code className="font-mono">pg_restore</code>
            {pgVersion ? ` версии не ниже ${pgVersion.split(".")[0]}` : ""}; дамп снят без
            владельцев и прав (--no-owner --no-privileges), расширения из списка
            создать заранее. Проверка:{" "}
            <code className="font-mono">sha256sum {result.filename}</code>
          </dd>
        </dl>
      )}
    </li>
  );
}
