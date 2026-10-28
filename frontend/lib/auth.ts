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

declare module "next-auth" {
  interface Session {
    accessToken?: string;
    error?: string;
  }
}
