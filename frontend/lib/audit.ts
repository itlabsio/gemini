import type { AuditRecord, Me } from "@/lib/types";

/** Период выборки журнала (?period=); по умолчанию — 7d. */
export type Period = "24h" | "7d" | "30d" | "90d" | "all" | "custom";

export const PERIODS: { value: Period; label: string }[] = [
  { value: "24h", label: "24 часа" },
  { value: "7d", label: "7 дней" },
  { value: "30d", label: "30 дней" },
  { value: "90d", label: "90 дней" },
  { value: "all", label: "Всё время" },
  { value: "custom", label: "Свой период" },
];

/** Тип действия — цвет метки в журнале. */
export type AuditKind = "create" | "update" | "delete" | "run" | "access" | "other";

/** Коды действий backend'а (s.audit) → подпись и тип. Неизвестный код показывается как есть. */
export const AUDIT_ACTIONS: Record<string, { label: string; kind: AuditKind }> = {
  "instance.create": { label: "Создание инстанса", kind: "create" },
  "instance.update": { label: "Изменение инстанса", kind: "update" },
  "instance.delete": { label: "Удаление инстанса", kind: "delete" },
  "instance.discover": { label: "Discovery баз", kind: "run" },
  "instance.unmanage": { label: "Снятие метки Terraform с инстанса", kind: "update" },
  "database.patch": { label: "Изменение базы", kind: "update" },
  "database.delete": { label: "Удаление базы", kind: "delete" },
  "database.run.dump": { label: "Ручной dump", kind: "run" },
  "database.run.restore": { label: "Ручной restore", kind: "run" },
  "run.cancel": { label: "Отмена прогона", kind: "run" },
  "mapping.save": { label: "Сохранение сопоставления", kind: "update" },
  "mapping.delete": { label: "Удаление сопоставления", kind: "delete" },
  "pairing.issue-code": { label: "Выпуск кода pairing", kind: "create" },
  "pairing.init": { label: "Pairing с Source", kind: "create" },
  "pairing.revoke": { label: "Разрыв связи", kind: "delete" },
  "pairing.delete": { label: "Удаление связи", kind: "delete" },
  "project.create": { label: "Создание проекта", kind: "create" },
  "project.delete": { label: "Удаление проекта", kind: "delete" },
  "project.unmanage": { label: "Снятие метки Terraform с проекта", kind: "update" },
  "s3_bucket.create": { label: "Добавление S3-бакета", kind: "create" },
  "s3_bucket.update": { label: "Изменение S3-бакета", kind: "update" },
  "s3_bucket.delete": { label: "Удаление S3-бакета", kind: "delete" },
  "settings.update": { label: "Изменение настроек", kind: "update" },
  "vault.inspect": { label: "Просмотр ключей Vault", kind: "access" },
  "access.denied": { label: "Отказ в доступе", kind: "access" },
  "audit.export": { label: "Экспорт журнала аудита", kind: "access" },
};

export const AUDIT_KIND_VARIANT = {
  create: "success",
  update: "info",
  delete: "warning",
  run: "secondary",
  access: "destructive",
  other: "outline",
} as const satisfies Record<AuditKind, string>;

export function auditAction(code: string) {
  return AUDIT_ACTIONS[code] ?? { label: code, kind: "other" as const };
}

/** Имя объекта: найденное по target, иначе из detail (удалённые объекты), иначе target. */
export function auditObject(r: AuditRecord): { name: string; deleted: boolean } {
  if (r.object_name) return { name: r.object_name, deleted: false };
  const d = r.detail ?? {};
  for (const k of ["name", "db_name", "source_db_name"]) {
    if (typeof d[k] === "string" && d[k]) return { name: d[k] as string, deleted: r.action.endsWith(".delete") };
  }
  return { name: r.target || "—", deleted: false };
}

/** Детали одной строкой: «ключ: значение» через запятую; для отказа — причина. */
export function auditSummary(r: AuditRecord): string {
  const d = r.detail ?? {};
  if (r.action === "access.denied" && typeof d.reason === "string") return d.reason;
  return Object.entries(d)
    .filter(([k]) => !["name", "db_name", "source_db_name"].includes(k))
    .map(([k, v]) => `${k}: ${typeof v === "string" ? v : JSON.stringify(v)}`)
    .join(", ");
}

/** Только роль security (ИБ): ни superuser, ни ролей в проектах — ей доступен лишь аудит. */
export function isSecurityOnly(me: Me): boolean {
  return me.security && !me.superuser && Object.keys(me.projects ?? {}).length === 0;
}
