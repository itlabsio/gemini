"use client";

import { useState } from "react";
import { CircleDashed, LoaderCircle, Square } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

/**
 * Статус активного прогона, который сам же и останавливает его: в покое
 * выглядит как бейдж running/pending, при наведении краснеет и превращается
 * в «Остановить». Остановка — только после подтверждения.
 */
export function RunningStopButton({
  status,
  kind,
  dbName,
  disabled,
  onStop,
}: {
  status: string;
  kind: string;
  dbName: string;
  disabled?: boolean;
  onStop: () => void;
}) {
  const [open, setOpen] = useState(false);
  const isRestore = kind === "restore";
  const Icon = status === "running" ? LoaderCircle : CircleDashed;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={disabled}
        title="Остановить прогон"
        className={cn(
          "group inline-flex min-w-[7.5rem] items-center justify-center gap-1 rounded-md border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-colors",
          "focus-visible:ring-[3px] focus-visible:ring-destructive/40 focus-visible:outline-none",
          "disabled:pointer-events-none disabled:opacity-50",
          status === "running"
            ? "bg-info/12 text-info dark:bg-info/20"
            : "bg-warning/15 text-warning dark:bg-warning/20",
          "hover:bg-destructive hover:text-destructive-foreground focus-visible:bg-destructive focus-visible:text-destructive-foreground",
          "[&_svg]:size-3",
        )}
      >
        <span className="inline-flex items-center gap-1 group-hover:hidden group-focus-visible:hidden">
          <Icon className={status === "running" ? "animate-spin" : undefined} />
          {status}
        </span>
        <span className="hidden items-center gap-1 group-hover:inline-flex group-focus-visible:inline-flex">
          <Square className="fill-current" />
          Остановить
        </span>
      </button>

      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Остановить {isRestore ? "восстановление" : "бэкап"} «{dbName}»?
            </AlertDialogTitle>
            <AlertDialogDescription>
              {isRestore ? (
                <>
                  Job будет удалён вместе с подом, прогон закроется с ошибкой.
                  Восстановление прервётся на середине — целевая база может
                  остаться в промежуточном состоянии до следующего restore
                </>
              ) : (
                <>
                  Job будет удалён вместе с подом, прогон закроется с ошибкой.
                  Недозалитый дамп не станет доступен Target’ам: без файла
                  контрольной суммы его никто не подхватит
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setOpen(false);
                onStop();
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Остановить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
