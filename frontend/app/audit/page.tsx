import type { Metadata } from "next";
import Link from "next/link";
import { ChevronRight, Download, ListRestart } from "lucide-react";
import { ApiError, listAudit, listAuditActors } from "@/lib/api";
import type { AuditPage } from "@/lib/types";
import { PERIODS, type Period } from "@/lib/audit";
import { AuditFilters } from "@/components/audit-filters";
import { AuditTable } from "@/components/audit-table";
import { PageHeader } from "@/components/ui/page-header";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export const metadata: Metadata = { title: "Аудит" };

type SearchParams = {
  period?: string;
  from?: string;
  to?: string;
  actor?: string;
  action?: string;
  q?: string;
  before?: string;
};

const DAY = 24 * 60 * 60 * 1000;
const PERIOD_MS: Record<Exclude<Period, "all" | "custom">, number> = {
  "24h": DAY,
  "7d": 7 * DAY,
  "30d": 30 * DAY,
  "90d": 90 * DAY,
};

/** Фильтры страницы → query backend'а (from/to — RFC3339). */
function filterQuery(sp: SearchParams, period: Period): URLSearchParams {
  const qs = new URLSearchParams();
  if (period === "custom") {
    if (sp.from) qs.set("from", sp.from);
    if (sp.to) qs.set("to", sp.to);
  } else if (period !== "all") {
    qs.set("from", new Date(Date.now() - PERIOD_MS[period]).toISOString());
  }
  if (sp.actor) qs.set("actor", sp.actor);
  if (sp.action) qs.set("action", sp.action);
  if (sp.q) qs.set("q", sp.q);
  return qs;
}

export default async function AuditLogPage({
  searchParams,
}: {
  searchParams: Promise<SearchParams>;
}) {
  const sp = await searchParams;
  const period: Period = PERIODS.some((p) => p.value === sp.period)
    ? (sp.period as Period)
    : "7d";
  const filters = filterQuery(sp, period);
  const pageQs = new URLSearchParams(filters);
  if (sp.before) pageQs.set("before", sp.before);

  let page: AuditPage = { items: [], next_before: 0 };
  let actors: string[] = [];
  let error: string | null = null;
  try {
    [page, actors] = await Promise.all([listAudit(`?${pageQs}`), listAuditActors()]);
  } catch (e) {
    error =
      e instanceof ApiError && e.status === 403
        ? "Журнал аудита доступен ролям superuser и security"
        : e instanceof Error
          ? e.message
          : "Ошибка загрузки";
  }

  // Ссылки пагинации сохраняют фильтры страницы (period/from/to — как в URL).
  const linkWith = (before?: number) => {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(sp)) if (v && k !== "before") qs.set(k, v);
    if (before) qs.set("before", String(before));
    const s = qs.toString();
    return s ? `/audit?${s}` : "/audit";
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Журнал аудита"
        description="Кто, что и когда менял или запускал в этом инстансе Gemini, и отказы в доступе"
        actions={
          !error && (
            <Button asChild variant="outline" size="sm">
              {/* Обычная ссылка, не Link: ответ — файл, а не страница. */}
              <a href={`/audit/export?${filters}`} download>
                <Download />
                Экспорт CSV
              </a>
            </Button>
          )
        }
      />

      <AuditFilters
        actors={actors}
        period={period}
        from={sp.from}
        to={sp.to}
        actor={sp.actor}
        action={sp.action}
        q={sp.q}
      />

      {error ? (
        <Alert variant="destructive">{error}</Alert>
      ) : (
        <>
          <AuditTable items={page.items} />
          {(sp.before || page.next_before > 0) && (
            <div className="flex items-center justify-between gap-3">
              {sp.before ? (
                <Button asChild variant="outline" size="sm">
                  <Link href={linkWith()}>
                    <ListRestart />
                    К последним
                  </Link>
                </Button>
              ) : (
                <span />
              )}
              {page.next_before > 0 && (
                <Button asChild variant="outline" size="sm">
                  <Link href={linkWith(page.next_before)}>
                    Более ранние
                    <ChevronRight />
                  </Link>
                </Button>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
