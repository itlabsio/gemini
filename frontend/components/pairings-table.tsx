"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import { Trash2, Unlink } from "lucide-react";
import type { HeadPairing } from "@/lib/types";
import { LocalDateTime } from "@/components/local-date-time";
import { deletePairingAction, revokePairingAction } from "@/app/pairing/actions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/status-badge";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function PairingsTable({
  pairings,
  canDelete,
}: {
  pairings: HeadPairing[];
  canDelete: boolean;
}) {
  const [target, setTarget] = useState<HeadPairing | null>(null);
  const [pending, start] = useTransition();

  // active-связь нельзя удалить из истории — её сначала разрывают (revoke).
  const isRevoke = target?.status === "active";

  function confirmAction() {
    if (!target) return;
    const id = target.id;
    start(async () => {
      const res = isRevoke
        ? await revokePairingAction(id)
        : await deletePairingAction(id);
      if (res.ok) toast.success(isRevoke ? "Связь разорвана" : "Связь удалена");
      else toast.error(res.error);
      setTarget(null);
    });
  }

  return (
    <>
      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Пир</TableHead>
              <TableHead>URL</TableHead>
              <TableHead>Статус</TableHead>
              <TableHead>Установлена</TableHead>
              {canDelete && <TableHead className="text-right" />}
            </TableRow>
          </TableHeader>
          <TableBody>
            {pairings.map((p) => (
              <TableRow key={p.id}>
                <TableCell>
                  <Badge variant="secondary" className="capitalize">
                    {p.peer_role}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {p.peer_url || "—"}
                </TableCell>
                <TableCell>
                  <StatusBadge status={p.status} />
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  <LocalDateTime value={p.established_at} />
                </TableCell>
                {canDelete && (
                  <TableCell className="text-right">
                    {p.status === "active" ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="Разорвать связь"
                        onClick={() => setTarget(p)}
                      >
                        <Unlink />
                      </Button>
                    ) : (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="Удалить связь"
                        onClick={() => setTarget(p)}
                      >
                        <Trash2 />
                      </Button>
                    )}
                  </TableCell>
                )}
              </TableRow>
            ))}
            {pairings.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={canDelete ? 5 : 4}
                  className="py-10 text-center text-muted-foreground"
                >
                  Связей нет
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <AlertDialog
        open={target !== null}
        onOpenChange={(o) => !o && setTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {isRevoke ? "Разорвать связь?" : "Удалить связь?"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {isRevoke ? (
                <>
                  Общий секрет пары «{target?.peer_role}» будет стёрт. Обмен с
                  этим контуром прекратится: подписанные запросы пира начнут
                  отбиваться. Вторую голову нужно будет разорвать отдельно, а для
                  повторного соединения — пройти pairing заново с обеих сторон
                </>
              ) : (
                <>
                  Запись «{target?.peer_role} · {target?.status}» будет удалена из
                  списка. Повторный pairing можно будет запустить заново
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                confirmAction();
              }}
              disabled={pending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {pending
                ? isRevoke
                  ? "Разрыв…"
                  : "Удаление…"
                : isRevoke
                  ? "Разорвать"
                  : "Удалить"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
