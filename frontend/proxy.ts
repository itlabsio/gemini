import { NextResponse } from "next/server";
import { auth } from "@/lib/auth";
import type { NextAuthRequest } from "next-auth";

// Единственное место, где проверяется сессия для защищённых страниц —
// см. комментарий в itlabs-acme-dns/frontend/proxy.ts про редирект-цикл.
const PUBLIC_PATHS = ["/"];

export const proxy = auth((request: NextAuthRequest) => {
  const { pathname } = request.nextUrl;
  const isPublic = PUBLIC_PATHS.includes(pathname);

  if (!isPublic) {
    const session = request.auth;
    if (!session || session.error === "RefreshAccessTokenError") {
      return NextResponse.redirect(new URL("/", request.url));
    }
  }

  const nonce = crypto.randomUUID().replace(/-/g, "");
  const csp = [
    `default-src 'self'`,
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`,
    `style-src 'self' 'unsafe-inline'`,
    `img-src 'self' data:`,
    `font-src 'self'`,
    `connect-src 'self'`,
    `object-src 'none'`,
    `base-uri 'self'`,
    `form-action 'self'`,
    `frame-ancestors 'none'`,
    `upgrade-insecure-requests`,
  ].join("; ");

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", csp);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", csp);
  response.headers.set("X-Content-Type-Options", "nosniff");
  response.headers.set("X-Frame-Options", "DENY");
  response.headers.set("Referrer-Policy", "strict-origin-when-cross-origin");
  response.headers.set(
    "Permissions-Policy",
    "camera=(), microphone=(), geolocation=(), payment=()"
  );
  response.headers.set(
    "Strict-Transport-Security",
    "max-age=63072000; includeSubDomains"
  );
  return response;
});

export const config = {
  matcher: [
    "/((?!api/auth|api/health|_next/static|_next/image|favicon.ico|favicon.svg).*)",
  ],
};
