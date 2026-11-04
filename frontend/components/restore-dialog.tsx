"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import { Loader2, Play } from "lucide-react";
import type { RestoreCandidate, RestoreMode } from "@/lib/types";
import { listRestoreCandidatesAction, runDatabaseAction } from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { LocalDateTime } from "@/components/local-date-time";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn, fmtBytes } from "@/lib/utils";

/** Имя файла дампа без префикса <инстанс>/<база>/. */
const baseName = (key: string) => key.slice(key.lastIndexOf("/") + 1);

const MODE_WARNING: Record<RestoreMode, string> = {
  recreate:
    "База будет пересоздана из дампа: текущая переименовывается и удаляется после успешного наката.",
  in_place:
    "Объекты из дампа перезапишут существующие (pg_restore --clean): данные базы заменятся содержимым дампа.",
  in_place_per_db:
    "Объекты из дампа перезапишут существующие (pg_restore --clean): данные базы заменятся содержимым дампа.",
};

interface Props {
  instanceId: string;
  databaseId: string;
  dbName: string;
  restoreMode?: RestoreMode;
  disabled?: boolean;
  /** Restore создан — таблица подхватывает активный прогон. */
  onStarted: () => void;
  className?: string;
}

/**
 * Кнопка ручного restore на target и диалог выбора дампа: последние дампы под
 * префиксом сопоставления, по умолчанию — самый свежий подписанный. Подпись
 * Source сверяется на backend'е при запуске.
 */
export function RestoreButton({
  instanceId,
  databaseId,
  dbName,
  restoreMode,
  disabled,
  onStarted,
  className,
}: Props) {
  const [open, setOpen] = useState(false);
  const [candidates, setCandidates] = useState<RestoreCandidate[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState("");
  const [loading, startLoad] = useTransition();
  const [starting, startRun] = useTransition();

  const load = () =>
    startLoad(async () => {
      setError(null);
      setCandidates(null);
      const res = await listRestoreCandidatesAction(databaseId);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setCandidates(res.candidates);
      setSelected(res.candidates.find((c) => c.signed)?.key ?? "");
    });

  const onOpenChange = (next: boolean) => {
    if (starting) return;
    setOpen(next);
    if (next) load();
  };

  const onRestore = () =>
    startRun(async () => {
      const res = await runDatabaseAction(instanceId, databaseId, selected);
      if (!res.ok) {
        toast.error(res.error);
        return;
      }
      toast.success(`Restore запущен · ${baseName(selected)}`);
      setOpen(false);
      onStarted();
    });

  const newestKey = candidates?.[0]?.key;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Button
        size="sm"
        variant="outline"
        onClick={() => onOpenChange(true)}
        disabled={disabled}
        className={className}
      >
        <Play />
        Запустить
      </Button>

      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Восстановление {dbName}</DialogTitle>
          <DialogDescription>
            Выберите дамп: показаны последние готовые дампы source-базы из S3.
          </DialogDescription>
        </DialogHeader>

        {error && <Alert variant="destructive">{error}</Alert>}

        {loading && !candidates ? (
          <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Загрузка списка дампов…
          </div>
        ) : candidates && candidates.length === 0 ? (
          <p className="py-6 text-sm text-muted-foreground">Готовых дампов в S3 нет.</p>
        ) : (
          candidates && (
            <fieldset
              disabled={starting}
              className="flex max-h-[45vh] flex-col divide-y overflow-auto rounded-md border"
            >
              <legend className="sr-only">Дамп для восстановления</legend>
              {candidates.map((c) => (
                <label
                  key={c.key}
                  title={c.signed ? c.key : "Рядом нет подписи Source (.sig) — восстановить нельзя"}
                  className={cn(
                    "flex items-center gap-3 p-3",
                    c.signed ? "cursor-pointer hover:bg-accent/40" : "cursor-not-allowed opacity-60",
                    selected === c.key && "bg-accent/50",
                  )}
                >
                  <input
                    type="radio"
                    name={`restore-${databaseId}`}
                    value={c.key}
                    checked={selected === c.key}
                    disabled={!c.signed}
                    onChange={() => setSelected(c.key)}
                    className="size-4 accent-primary"
                  />
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate font-mono text-xs">{baseName(c.key)}</span>
                    <span className="text-xs text-muted-foreground">
                      <LocalDateTime value={c.last_modified} /> · {fmtBytes(c.size)}
                    </span>
                  </div>
                  {!c.signed ? (
                    <Badge variant="warning">без подписи</Badge>
                  ) : c.key === newestKey ? (
                    <Badge variant="info">последний</Badge>
                  ) : null}
                </label>
              ))}
            </fieldset>
          )
        )}

        {selected && (
          <Alert variant="destructive">
            {MODE_WARNING[restoreMode ?? "recreate"]}
            {selected !== newestKey && " Выбран не самый свежий дамп."}
          </Alert>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={starting}>
            Отмена
          </Button>
          <Button
            variant="destructive"
            onClick={onRestore}
            disabled={!selected || starting || loading}
          >
            {starting ? <Loader2 className="animate-spin" /> : <Play />}
            Восстановить
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
