"use client";

import { useState } from "react";
import { StatusBadge } from "@/components/status-badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/**
 * Ячейка статуса прогона. Сам статус — лаконичный бейдж; если есть текст
 * ошибки, бейдж кликабелен и открывает модалку с полным сообщением.
 */
export function RunStatusCell({
  status,
  error,
  title = "Детали ошибки",
}: {
  status?: string | null;
  error?: string | null;
  title?: string;
}) {
  const [open, setOpen] = useState(false);
  const hasError = Boolean(error && error.trim());

  if (!hasError) return <StatusBadge status={status} />;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="rounded-md focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        title="Показать ошибку"
      >
        <StatusBadge status={status} />
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {title}
              <StatusBadge status={status} />
            </DialogTitle>
            <DialogDescription>
              Сообщение от pg_dump / pg_restore или пира
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-[50vh] overflow-auto rounded-md border bg-muted/40 p-3 text-xs whitespace-pre-wrap break-words">
            {error}
          </pre>
        </DialogContent>
      </Dialog>
    </>
  );
}
