import type { Metadata } from "next";
import type { ReactNode } from "react";
import Link from "next/link";
import { auth } from "@/lib/auth";
import SessionGuard from "@/components/SessionGuard";
import { Nav } from "@/components/nav";
import { UserMenu } from "@/components/user-menu";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";
import { Toaster } from "@/components/ui/sonner";
import { getMe, getPublicConfig } from "@/lib/api";
import type { HeadRole } from "@/lib/types";
import "./globals.css";

export const metadata: Metadata = {
  title: "Gemini — СРК",
  description: "Система логического резервного копирования",
};

export default async function RootLayout({
  children,
}: Readonly<{ children: ReactNode }>) {
  const session = await auth();

  // Роль головы — свойство деплоя, не пользователя: тянем публично, чтобы бейдж
  // был верным и на странице логина (до токена).
  let headRole: HeadRole = "source";
  try {
    headRole = (await getPublicConfig()).head_role;
  } catch {
    /* backend недоступен — покажем базовый layout */
  }

  let role: string | undefined;
  if (session?.accessToken) {
    try {
      role = (await getMe()).role;
    } catch {
      /* backend недоступен — покажем базовый layout */
    }
  }

  return (
    <html lang="ru" className="h-full antialiased">
      <head>
        <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
      </head>
      <body className="min-h-full">
          <TooltipProvider>
            <SessionGuard expired={session?.error === "RefreshAccessTokenError"} />
            <div className="flex min-h-screen flex-col">
              <header className="sticky top-0 z-40 border-b bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
                <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4 sm:px-6">
                  <Link
                    href={session?.user ? "/instances" : "/"}
                    className="flex shrink-0 items-center gap-2 font-semibold tracking-tight"
                  >
                    <span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground text-xs font-bold">
                      G
                    </span>
                    Gemini
                    <Badge
                      variant={headRole === "target" ? "info" : "secondary"}
                      className="uppercase"
                    >
                      {headRole}
                    </Badge>
                  </Link>

                  {session?.user && (
                    <>
                      <div className="mx-1 h-6 w-px shrink-0 bg-border" />
                      {/* На узкой ширине шапка прокручивается по горизонтали;
                          логотип и меню аккаунта остаются на месте. */}
                      <div className="min-w-0 flex-1 overflow-x-auto [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
                        <Nav headRole={headRole} role={role} />
                      </div>
                      <div className="shrink-0">
                        <UserMenu
                          email={session.user.email ?? "—"}
                          role={role}
                        />
                      </div>
                    </>
                  )}
                </div>
              </header>

              <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-6">
                {children}
              </main>
            </div>
            <Toaster />
          </TooltipProvider>
      </body>
    </html>
  );
}
