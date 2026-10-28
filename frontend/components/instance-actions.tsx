"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";
import { toast } from "sonner";
import { MoreHorizontal, Pencil, Trash2, Unlink } from "lucide-react";
import { deleteInstanceAction, unmanageInstanceAction } from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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

export function InstanceActions({
  id,
  name,
  canUnmanage = false,
}: {
  id: string;
  name: string;
  /** superuser и инстанс под управлением Terraform. */
  canUnmanage?: boolean;
}) {
  const router = useRouter();
  const [pending, start] = useTransition();
  const [confirmOpen, setConfirmOpen] = useState(false);

  function onDelete() {
    start(async () => {
      const res = await deleteInstanceAction(id);
      if (res.ok) {
        toast.success(`Инстанс «${name}» удалён`);
        router.push("/instances");
      } else {
        toast.error(res.error);
        setConfirmOpen(false);
      }
    });
  }

  function onUnmanage() {
    start(async () => {
      const res = await unmanageInstanceAction(id);
      if (res.ok) toast.success("Метка Terraform снята");
      else toast.error(res.error);
    });
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="icon" aria-label="Действия">
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem asChild>
            <Link href={`/instances/${id}/edit`}>
              <Pencil />
              Изменить
            </Link>
          </DropdownMenuItem>
          {canUnmanage && (
            <DropdownMenuItem disabled={pending} onSelect={onUnmanage}>
              <Unlink />
              Снять метку Terraform
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onSelect={(e) => {
              e.preventDefault();
              setConfirmOpen(true);
            }}
          >
            <Trash2 />
            Удалить
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить инстанс «{name}»?</AlertDialogTitle>
            <AlertDialogDescription>
              Вместе с инстансом удалятся его базы и расписания. История запусков
              останется. Действие необратимо
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                onDelete();
              }}
              disabled={pending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {pending ? "Удаление…" : "Удалить"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
