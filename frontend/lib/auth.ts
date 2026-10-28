import NextAuth from "next-auth";
import Keycloak from "next-auth/providers/keycloak";

export const { handlers, auth, signIn, signOut } = NextAuth({
  trustHost: true,
  providers: [
    Keycloak({
      clientId: process.env.KEYCLOAK_CLIENT_ID!,
      clientSecret: process.env.KEYCLOAK_CLIENT_SECRET!,
      issuer: process.env.KEYCLOAK_URL!,
    }),
  ],
  callbacks: {
    // Права (superuser / роли по проектам из groups) решает backend по
    // access-токену; фронт берёт их из /api/me.
    async jwt({ token, account }) {
      if (account) {
        token.accessToken = account.access_token;
        token.refreshToken = account.refresh_token;
        token.expiresAt = account.expires_at;
      }
      if (Date.now() < (token.expiresAt as number) * 1000 - 30_000) return token;
      return await refreshAccessToken(token);
    },
    async session({ session, token }) {
      session.accessToken = token.accessToken as string | undefined;
      session.error = token.error as string | undefined;
      return session;
    },
  },
});

// auth() зовётся из proxy, layout и страницы одного запроса — каждый видит одну
// и ту же cookie со старым refresh-токеном. При включённой в Keycloak ротации
// («Revoke Refresh Token») второй обмен того же токена отклоняется и разлогинивает
// пользователя. Поэтому обмен делается один раз на refresh-токен: параллельные
// вызовы ждут общий промис, а успешный результат переиспользуется ещё REFRESH_REUSE_MS
// (запросы, прилетевшие со старой cookie чуть позже). Состояние — в памяти процесса
// (standalone-сервер — один процесс); при нескольких репликах без sticky-сессий
// гонка между репликами остаётся.
const REFRESH_REUSE_MS = 60_000;
const refreshes = new Map<
  string,
  { promise: Promise<Record<string, unknown>>; settledAt?: number }
>();

function refreshAccessToken(token: Record<string, unknown>): Promise<Record<string, unknown>> {
  const key = token.refreshToken as string;
  const now = Date.now();
  for (const [k, v] of refreshes) {
    if (v.settledAt && now - v.settledAt > REFRESH_REUSE_MS) refreshes.delete(k);
  }

  const cached = refreshes.get(key);
  if (cached) return cached.promise.then((fresh) => ({ ...token, ...fresh }));

  const entry: { promise: Promise<Record<string, unknown>>; settledAt?: number } = {
    promise: doRefresh(token),
  };
  refreshes.set(key, entry);
  entry.promise.then((fresh) => {
    // Ошибку не кэшируем: следующий запрос должен попробовать заново.
    if (fresh.error) refreshes.delete(key);
    else entry.settledAt = Date.now();
  });
  return entry.promise;
}

async function doRefresh(token: Record<string, unknown>): Promise<Record<string, unknown>> {
  try {
    const url = `${process.env.KEYCLOAK_URL}/protocol/openid-connect/token`;
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        grant_type: "refresh_token",
        client_id: process.env.KEYCLOAK_CLIENT_ID!,
        client_secret: process.env.KEYCLOAK_CLIENT_SECRET!,
        refresh_token: token.refreshToken as string,
      }),
    });
    const data = await res.json();
    if (!res.ok) throw data;
    return {
      ...token,
      accessToken: data.access_token,
      expiresAt: Math.floor(Date.now() / 1000) + data.expires_in,
      refreshToken: data.refresh_token ?? token.refreshToken,
      error: undefined,
    };
  } catch (err) {
    console.error("refresh token failed", err);
    return { ...token, error: "RefreshAccessTokenError" };
  }
}

declare module "next-auth" {
  interface Session {
    accessToken?: string;
    error?: string;
  }
}
