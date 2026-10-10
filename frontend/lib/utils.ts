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

// Множители суффиксов k8s-величин в байтах (двоичные и десятичные).
const BYTE_SUFFIX: Record<string, number> = {
  Ki: 2 ** 10, Mi: 2 ** 20, Gi: 2 ** 30, Ti: 2 ** 40, Pi: 2 ** 50, Ei: 2 ** 60,
  k: 1e3, M: 1e6, G: 1e9, T: 1e12, P: 1e15, E: 1e18,
};
const BYTES_RE = /^(\d+(?:\.\d+)?)([A-Za-z]*)$/;

/**
 * Ошибка в k8s-величине объёма (память, диск) или null, если всё в порядке.
 * Голое число k8s читает как байты — почти всегда это забытый суффикс, поэтому
 * единица обязательна. Пустая строка валидна (= значение по умолчанию).
 */
export function bytesQuantityError(v: string, opts?: { min?: string }): string | null {
  const s = v.trim();
  if (!s) return null;
  const m = BYTES_RE.exec(s);
  if (!m) return "Неверный формат, пример: 20Gi";
  if (!m[2]) return "Укажите единицу: Mi, Gi…";
  const mult = BYTE_SUFFIX[m[2]];
  if (!mult) return `Неизвестная единица «${m[2]}»: Ki, Mi, Gi, Ti`;
  if (opts?.min) {
    const min = BYTES_RE.exec(opts.min)!;
    if (Number(m[1]) * mult < Number(min[1]) * BYTE_SUFFIX[min[2]]) {
      return `Не меньше ${opts.min}`;
    }
  }
  return null;
}

/** Минимальный размер тома дампа — как k8s.MinWorkDirSize на бэкенде. */
export const MIN_STORAGE_SIZE = "1Gi";
