"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Search } from "lucide-react";
import { AUDIT_ACTIONS, PERIODS, type Period } from "@/lib/audit";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Значения Select не могут быть пустыми; «/» не встречается ни в email, ни в
// кодах действий, так что служебное значение ни с чем не пересечётся.
const ALL = "/all";

/** ISO-момент → YYYY-MM-DD в поясе браузера (для input type=date). */
function toDateInput(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** YYYY-MM-DD (локальная полночь) → ISO; endOfDay — начало следующих суток. */
function fromDateInput(v: string, endOfDay = false): string | undefined {
  if (!v) return undefined;
  const [y, m, d] = v.split("-").map(Number);
  return new Date(y, m - 1, d + (endOfDay ? 1 : 0)).toISOString();
}

/** Фильтры журнала аудита: состояние живёт в URL, смена фильтра сбрасывает пагинацию. */
export function AuditFilters({
  actors,
  period,
  from,
  to,
  actor,
  action,
  q,
}: {
  actors: string[];
  period: Period;
  from?: string;
  to?: string;
  actor?: string;
  action?: string;
  q?: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const [search, setSearch] = useState(q ?? "");

  function update(patch: Record<string, string | undefined>) {
    const next = new URLSearchParams(params);
    for (const [k, v] of Object.entries(patch)) {
      if (v) next.set(k, v);
      else next.delete(k);
    }
    next.delete("before");
    const s = next.toString();
    router.push(s ? `${pathname}?${s}` : pathname);
  }

  // Поиск — с задержкой, чтобы не дёргать backend на каждую букву.
  useEffect(() => {
    if (search === (q ?? "")) return;
    const t = setTimeout(() => update({ q: search.trim() || undefined }), 400);
    return () => clearTimeout(t);
    // update меняется на каждый рендер; реагируем только на ввод.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search]);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={period}
        onValueChange={(v) =>
          update({ period: v === "7d" ? undefined : v, from: undefined, to: undefined })
        }
      >
        <SelectTrigger size="sm" className="w-40" aria-label="Период">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {PERIODS.map((p) => (
            <SelectItem key={p.value} value={p.value}>
              {p.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {period === "custom" && (
        <>
          <Input
            type="date"
            aria-label="С даты"
            className="h-8 w-40"
            defaultValue={toDateInput(from)}
            onChange={(e) => update({ from: fromDateInput(e.target.value) })}
          />
          <span className="text-muted-foreground">—</span>
          <Input
            type="date"
            aria-label="По дату включительно"
            className="h-8 w-40"
            defaultValue={toDateInput(to ? new Date(new Date(to).getTime() - 1).toISOString() : undefined)}
            onChange={(e) => update({ to: fromDateInput(e.target.value, true) })}
          />
        </>
      )}

      <Select value={actor ?? ALL} onValueChange={(v) => update({ actor: v === ALL ? undefined : v })}>
        <SelectTrigger size="sm" className="w-56" aria-label="Пользователь">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>Все пользователи</SelectItem>
          {actors.map((a) => (
            <SelectItem key={a} value={a}>
              {a}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select value={action ?? ALL} onValueChange={(v) => update({ action: v === ALL ? undefined : v })}>
        <SelectTrigger size="sm" className="w-64" aria-label="Действие">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>Все действия</SelectItem>
          {Object.entries(AUDIT_ACTIONS).map(([code, a]) => (
            <SelectItem key={code} value={code}>
              {a.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <div className="relative min-w-48 flex-1">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Поиск по объекту или деталям"
          aria-label="Поиск по объекту или деталям"
          className="h-8 pl-8"
        />
      </div>
    </div>
  );
}
