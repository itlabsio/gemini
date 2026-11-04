"use client";

import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";
import { toast } from "sonner";
import { ShieldCheck, ShieldOff } from "lucide-react";
import { setInstanceRestorableAction } from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
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
 * Предохранитель target-инстанса (superuser): «Отключить восстановление»
 * запрещает любой restore в его базы — и автоматический, и ручной. Настройки
 * автовосстановления баз сохраняются и снова действуют после разрешения.
 */
export function RestorableToggle({
  id,
  name,
  restorable,
}: {
  id: string;
  name: string;
  restorable: boolean;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [pending, start] = useTransition();

  const apply = () =>
    start(async () => {
      const res = await setInstanceRestorableAction(id, !restorable);
      if (!res.ok) {
        toast.error(res.error);
        return;
      }
      toast.success(restorable ? "Восстановление отключено" : "Восстановление разрешено");
      setOpen(false);
      router.refresh();
    });

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
        className={restorable ? "text-destructive hover:text-destructive" : undefined}
      >
        {restorable ? <ShieldOff /> : <ShieldCheck />}
        {restorable ? "Отключить восстановление" : "Разрешить восстановление"}
      </Button>

      <AlertDialog open={open} onOpenChange={(v) => !pending && setOpen(v)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {restorable ? "Отключить восстановление" : "Разрешить восстановление"} в «{name}»?
            </AlertDialogTitle>
            <AlertDialogDescription>
              {restorable
                ? "Restore в базы инстанса будет запрещён полностью: ни по webhook'у и поллингу, ни вручную. Уже идущие прогоны не останавливаются. Настройки автовосстановления баз сохранятся."
                : "Базы снова можно восстанавливать вручную, а с включённым автовосстановлением — автоматически, как до отключения."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                apply();
              }}
              disabled={pending}
              className={
                restorable
                  ? "bg-destructive text-destructive-foreground hover:bg-destructive/90"
                  : undefined
              }
            >
              {restorable ? "Отключить" : "Разрешить"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
