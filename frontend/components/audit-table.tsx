"use client";

import { useState } from "react";
import type { AuditRecord } from "@/lib/types";
import { AUDIT_KIND_VARIANT, auditAction, auditObject, auditSummary } from "@/lib/audit";
import { fmtDateTime } from "@/lib/utils";
import { LocalDateTime } from "@/components/local-date-time";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

function ActionBadge({ code }: { code: string }) {
  const a = auditAction(code);
  return (
    <Badge variant={AUDIT_KIND_VARIANT[a.kind]} title={code}>
      {a.label}
    </Badge>
  );
}

/** Таблица журнала аудита; клик по строке — все поля записи. */
export function AuditTable({ items }: { items: AuditRecord[] }) {
  const [open, setOpen] = useState<AuditRecord | null>(null);

  return (
    <>
      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Время</TableHead>
              <TableHead>Пользователь</TableHead>
              <TableHead>Действие</TableHead>
              <TableHead>Объект</TableHead>
              <TableHead>Детали</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((r) => {
              const obj = auditObject(r);
              return (
                <TableRow
                  key={r.id}
                  tabIndex={0}
                  className="cursor-pointer"
                  onClick={() => setOpen(r)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      setOpen(r);
                    }
                  }}
                >
                  <TableCell
                    className="whitespace-nowrap text-muted-foreground"
                    title={`${fmtDateTime(r.created_at, { utc: true })} UTC`}
                  >
                    <LocalDateTime value={r.created_at} />
                  </TableCell>
                  <TableCell className="max-w-[14rem] truncate" title={r.ip ? `IP ${r.ip}` : undefined}>
                    {r.actor || "—"}
                  </TableCell>
                  <TableCell>
                    <ActionBadge code={r.action} />
                  </TableCell>
                  <TableCell className="max-w-[16rem] truncate" title={r.target}>
                    {obj.name}
                    {obj.deleted && <span className="ml-1 text-muted-foreground">(удалён)</span>}
                  </TableCell>
                  <TableCell className="max-w-xs truncate text-muted-foreground">
                    {auditSummary(r) || "—"}
                  </TableCell>
                </TableRow>
              );
            })}
            {items.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-10 text-center text-muted-foreground">
                  За выбранный период записей нет
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog open={open !== null} onOpenChange={(o) => !o && setOpen(null)}>
        <DialogContent className="sm:max-w-2xl">
          {open && (
            <>
              <DialogHeader>
                <DialogTitle className="flex items-center gap-2">
                  <ActionBadge code={open.action} />
                  <span className="font-mono text-sm text-muted-foreground">{open.action}</span>
                </DialogTitle>
                <DialogDescription>Запись #{open.id}</DialogDescription>
              </DialogHeader>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                <dt className="text-muted-foreground">Время</dt>
                <dd>
                  <LocalDateTime value={open.created_at} />{" "}
                  <span className="text-muted-foreground">
                    ({fmtDateTime(open.created_at, { utc: true })} UTC)
                  </span>
                </dd>
                <dt className="text-muted-foreground">Пользователь</dt>
                <dd className="break-all">{open.actor || "—"}</dd>
                <dt className="text-muted-foreground">Объект</dt>
                <dd className="break-all">{auditObject(open).name}</dd>
                <dt className="text-muted-foreground">Target</dt>
                <dd className="break-all font-mono text-xs">{open.target || "—"}</dd>
                <dt className="text-muted-foreground">IP</dt>
                <dd className="font-mono text-xs">{open.ip || "—"}</dd>
                <dt className="text-muted-foreground">User-Agent</dt>
                <dd className="break-all text-xs">{open.user_agent || "—"}</dd>
              </dl>
              {Object.keys(open.detail ?? {}).length > 0 && (
                <pre className="max-h-72 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">
                  {JSON.stringify(open.detail, null, 2)}
                </pre>
              )}
            </>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
