import type { NextRequest } from "next/server";
import { handlers } from "@/lib/auth";

/**
 * GET /api/auth/session отдаёт объект сессии в браузер. В нём лежит
 * `accessToken` — полноценный ключ к backend API (Ingress выставляет /api
 * наружу), а нужен он только серверному коду в lib/api.ts через auth().
 *
 * Вырезаем поле из ответа эндпоинта, оставляя его в зашифрованной JWT-cookie:
 * так refresh-логика в callbacks.jwt продолжает работать как была, а браузер
 * токена не видит ни в разметке страницы, ни через fetch.
 */
export async function GET(req: NextRequest) {
  const res = await handlers.GET(req);

  if (!new URL(req.url).pathname.endsWith("/session")) return res;
  if (!res.headers.get("content-type")?.includes("application/json")) return res;

  const body = await res.clone().json().catch(() => null);
  if (!body || typeof body !== "object" || !("accessToken" in body)) return res;

  delete (body as Record<string, unknown>).accessToken;

  // Заголовки переносим как есть (важен Set-Cookie от ротации сессии),
  // кроме content-length — тело стало короче.
  const headers = new Headers(res.headers);
  headers.delete("content-length");
  return new Response(JSON.stringify(body), { status: res.status, headers });
}

export const POST = handlers.POST;
