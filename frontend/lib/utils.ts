import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/**
 * Дата-время в русской локали; пустое значение → «—».
 *
 * `utc: true` фиксирует пояс UTC — нужно для детерминированного первого рендера
 * в SSR/гидрации; браузерный пояс подставляет <LocalDateTime> после монтирования.
 */
export function fmtDateTime(ts?: string | null, opts?: { utc?: boolean }): string {
  if (!ts) return "—";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("ru-RU", opts?.utc ? { timeZone: "UTC" } : undefined);
}

/**
 * server_version_num → человекочитаемая версия PostgreSQL: 170004 → «17.4».
 * 0 / undefined (discovery не выполнялся) → null.
 */
export function fmtPgVersion(n?: number | null): string | null {
  if (!n || n <= 0) return null;
  return `${Math.floor(n / 10000)}.${n % 10000}`;
}
