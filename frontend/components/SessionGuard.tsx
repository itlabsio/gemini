"use client";

import { signOut } from "next-auth/react";
import { useEffect } from "react";

/**
 * Дочищает протухшую сессию: refresh-токен умер, proxy уже увёл на "/", а
 * cookie осталась — без signOut следующий заход снова начнётся с редиректа.
 *
 * Флаг приходит пропом с сервера, а не из useSession(): тот тянул бы
 * /api/auth/session, то есть держал бы на странице SessionProvider с полным
 * объектом сессии. Ради одного булева это не нужно.
 */
export default function SessionGuard({ expired }: { expired: boolean }) {
  useEffect(() => {
    if (expired) signOut({ callbackUrl: "/" });
  }, [expired]);

  return null;
}
