import { fetchAuditExport } from "@/lib/api";

// Экспорт CSV идёт через сервер Next: у браузера нет access token'а для backend'а.
// Тело отдаётся потоком, без буферизации всей выгрузки в памяти.
export async function GET(request: Request) {
  const { search } = new URL(request.url);
  let res: Response;
  try {
    res = await fetchAuditExport(search);
  } catch {
    return new Response("Сессия истекла — войдите заново", { status: 401 });
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    return new Response(body.error ?? res.statusText, { status: res.status });
  }
  return new Response(res.body, {
    headers: {
      "Content-Type": res.headers.get("Content-Type") ?? "text/csv; charset=utf-8",
      "Content-Disposition": res.headers.get("Content-Disposition") ?? "attachment",
      "Cache-Control": "no-store",
    },
  });
}
