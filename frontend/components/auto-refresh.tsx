"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

/**
 * AutoRefresh дергает router.refresh() на интервале, пока active=true.
 * Нужен там, где серверные данные меняет ВТОРАЯ сторона (пир по HTTP), и у
 * страницы нет собственного триггера ре-рендера — например, Source ждёт, пока
 * Target завершит pairing. Как только на очередном refresh active станет false,
 * эффект снимется. Есть жёсткий потолок по времени, чтобы вкладка не опрашивала
 * бэкенд вечно.
 */
export function AutoRefresh({
  active,
  intervalMs = 4000,
  timeoutMs = 180_000,
}: {
  active: boolean;
  intervalMs?: number;
  timeoutMs?: number;
}) {
  const router = useRouter();
  useEffect(() => {
    if (!active) return;
    const startedAt = Date.now();
    const id = setInterval(() => {
      if (Date.now() - startedAt > timeoutMs) {
        clearInterval(id);
        return;
      }
      router.refresh();
    }, intervalMs);
    return () => clearInterval(id);
  }, [active, intervalMs, timeoutMs, router]);
  return null;
}
