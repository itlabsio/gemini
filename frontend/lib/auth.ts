import NextAuth from "next-auth";
import Keycloak from "next-auth/providers/keycloak";

export type Role = "viewer" | "operator" | "admin";

const ROLE_RANK: Record<Role, number> = { viewer: 1, operator: 2, admin: 3 };

/**
 * Роль берётся ТОЛЬКО из resource_access.<clientId>.roles (client-роли клиента
 * головы). realm-роли и groups не учитываются — иначе realm-роль "admin" у
 * realm-администратора поднимала бы его и здесь.
 */
function resolveRole(profile: unknown): Role | null {
  const p = (profile ?? {}) as Record<string, unknown>;
  const clientId = process.env.KEYCLOAK_CLIENT_ID!;
  const resource = p.resource_access as Record<string, { roles?: string[] }> | undefined;

  let best: Role | null = null;
  for (const name of resource?.[clientId]?.roles ?? []) {
    if (name in ROLE_RANK && (!best || ROLE_RANK[name as Role] > ROLE_RANK[best])) {
      best = name as Role;
    }
  }
  return best;
}

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
    async signIn({ profile }) {
      // Мягкий гейт: если client-роли в profile (ID token / userinfo) вообще нет —
      // не блокируем, решает backend по access-токену. Если есть — нужна валидная.
      const p = (profile ?? {}) as Record<string, unknown>;
      if (p.resource_access == null) return true;
      return resolveRole(profile) !== null;
    },
    async jwt({ token, account, profile }) {
      if (account) {
        token.accessToken = account.access_token;
        token.refreshToken = account.refresh_token;
        token.expiresAt = account.expires_at;
        token.role = resolveRole(profile) ?? undefined;
      }
      if (Date.now() < (token.expiresAt as number) * 1000 - 30_000) return token;
      return await refreshAccessToken(token);
    },
    async session({ session, token }) {
      session.accessToken = token.accessToken as string | undefined;
      session.error = token.error as string | undefined;
      session.role = token.role as Role | undefined;
      return session;
    },
  },
});

async function refreshAccessToken(token: Record<string, unknown>) {
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

export function roleAtLeast(role: Role | undefined, min: Role): boolean {
  return !!role && ROLE_RANK[role] >= ROLE_RANK[min];
}

declare module "next-auth" {
  interface Session {
    accessToken?: string;
    error?: string;
    role?: Role;
  }
}
