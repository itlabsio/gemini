"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import { X } from "lucide-react";
import type { DatabaseMapping } from "@/lib/types";
import { deleteMappingAction } from "@/app/mapping/actions";
import { Badge } from "@/components/ui/badge";
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

/** Как восстанавливается целевая база: авто, только вручную, запрещено на инстансе. */
export type RestoreState = "auto" | "manual" | "off";

const STATE_BADGE: Record<
  RestoreState,
  { label: string; variant: "success" | "secondary" | "destructive"; hint: string }
> = {
  auto: { label: "авто", variant: "success", hint: "Свежие дампы накатываются автоматически" },
  manual: {
    label: "вручную",
    variant: "secondary",
    hint: "Автовосстановление базы выключено — только вручную",
  },
  off: {
    label: "отключено",
    variant: "destructive",
    hint: "Восстановление в инстанс запрещено superuser",
  },
};

// Бейдж статуса восстановления; для admin при наведении поверх него появляется
// крестик, разрывающий сопоставление (после подтверждения).
export function MappingActiveBadge({
  mapping,
  state,
  sourceLabel,
  targetLabel,
  canUnlink,
}: {
  mapping: DatabaseMapping;
  /** undefined — целевая база недоступна пользователю или не найдена. */
  state?: RestoreState;
  sourceLabel: string;
  targetLabel: string;
  canUnlink: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [pending, start] = useTransition();

  const st = state ? STATE_BADGE[state] : null;
  const badge = (
    <Badge variant={st?.variant ?? "outline"} title={canUnlink ? undefined : st?.hint}>
      {st?.label ?? "—"}
    </Badge>
  );
  if (!canUnlink) return badge;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        title="Разорвать сопоставление"
        aria-label={`Разорвать сопоставление ${sourceLabel} → ${targetLabel}`}
        className="group relative inline-flex rounded-md outline-none focus-visible:ring-2 focus-visible:ring-destructive/50"
      >
        {badge}
        <span className="absolute inset-0 flex items-center justify-center rounded-md bg-destructive text-white opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
          <X className="size-3.5" />
        </span>
      </button>

      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Разорвать сопоставление?</AlertDialogTitle>
            <AlertDialogDescription>
              Дампы «{sourceLabel}» перестанут восстанавливаться в «{targetLabel}».
              Сама целевая база не изменится. Связь можно создать заново выше
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                start(async () => {
                  const res = await deleteMappingAction(mapping.id);
                  if (res.ok) {
                    toast.success(`${sourceLabel}: связь разорвана`);
                    setOpen(false);
                  } else {
                    toast.error(res.error);
                  }
                });
              }}
              disabled={pending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {pending ? "Разрыв…" : "Разорвать"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
