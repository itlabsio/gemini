"use client";

import Link from "next/link";
import { useState } from "react";
import type { BackupRun } from "@/lib/types";
import { cn } from "@/lib/utils";
import { LocalDateTime } from "@/components/local-date-time";
import { RunErrorDialog } from "@/components/run-status-cell";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

const BAR_COLOR: Record<string, string> = {
  succeeded: "bg-success",
  failed: "bg-destructive",
  running: "bg-info animate-pulse",
  pending: "bg-warning",
};

const STATUS_LABEL: Record<string, string> = {
  succeeded: "успешно",
  failed: "ошибка",
  running: "выполняется",
  pending: "в очереди",
};

const BAR = "h-5 w-1.5 shrink-0 rounded-[2px]";

function fmtDuration(from?: string, to?: string): string | null {
  if (!from || !to) return null;
  const sec = Math.round((new Date(to).getTime() - new Date(from).getTime()) / 1000);
  if (!Number.isFinite(sec) || sec < 0) return null;
  if (sec < 60) return `${sec} с`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} мин ${sec % 60} с`;
  return `${Math.floor(min / 60)} ч ${min % 60} мин`;
}

/**
 * Полоска последних прогонов базы: слева старые, справа свежие. Пустые слоты
 * дополняют её до slots, чтобы полоски в таблице были одной ширины. Упавший
 * прогон с текстом ошибки открывает модалку, остальные ведут в журнал базы.
 */
export function RunHistoryStrip({
  runs,
  slots,
  journalHref,
}: {
  /** Прогоны базы от новых к старым (как отдаёт API). */
  runs: BackupRun[];
  slots: number;
  journalHref: string;
}) {
  // Прогон держим и после закрытия, чтобы текст не пропадал во время анимации.
  const [errorRun, setErrorRun] = useState<BackupRun | null>(null);
  const [errorOpen, setErrorOpen] = useState(false);
  const shown = runs.slice(0, slots).reverse();
  const empty = slots - shown.length;

  return (
    <div className="flex items-center gap-0.5" aria-label="Последние запуски">
      {Array.from({ length: empty }, (_, i) => (
        <span key={`empty-${i}`} className={cn(BAR, "bg-muted")} />
      ))}
      {shown.map((r) => {
        const hasError = r.status === "failed" && Boolean(r.error_message?.trim());
        const bar = cn(
          BAR,
          BAR_COLOR[r.status] ?? "bg-muted-foreground",
          "transition-opacity hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        );
        const label = STATUS_LABEL[r.status] ?? r.status;
        return (
          <Tooltip key={r.id}>
            <TooltipTrigger asChild>
              {hasError ? (
                <button
                  type="button"
                  className={bar}
                  aria-label={`${label}: показать ошибку`}
                  onClick={() => {
                    setErrorRun(r);
                    setErrorOpen(true);
                  }}
                />
              ) : (
                <Link href={journalHref} className={bar} aria-label={label} />
              )}
            </TooltipTrigger>
            <TooltipContent>
              <div className="flex flex-col gap-0.5">
                <span className="font-medium">
                  {r.kind} · {label}
                </span>
                <LocalDateTime value={r.started_at ?? r.created_at} />
                <span>
                  {r.trigger}
                  {fmtDuration(r.started_at, r.finished_at) &&
                    ` · ${fmtDuration(r.started_at, r.finished_at)}`}
                </span>
                {hasError && <span>Нажмите, чтобы увидеть ошибку</span>}
              </div>
            </TooltipContent>
          </Tooltip>
        );
      })}

      <RunErrorDialog
        open={errorOpen}
        onOpenChange={setErrorOpen}
        status={errorRun?.status}
        error={errorRun?.error_message}
      />
    </div>
  );
}
