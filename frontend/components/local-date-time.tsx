"use client";

import { useSyncExternalStore } from "react";
import { fmtDateTime } from "@/lib/utils";

const noopSubscribe = () => () => {};

/**
 * Дата-время в часовом поясе браузера пользователя.
 *
 * Серверные компоненты форматируют дату в поясе процесса Next (обычно UTC), из-за
 * чего журнал показывал не локальное время. useSyncExternalStore отдаёт разные
 * снимки на сервере и на клиенте: первый рендер (SSR + гидрация) детерминированно
 * идёт в UTC, а после гидрации React перерисовывает уже в поясе браузера.
 */
export function LocalDateTime({ value }: { value?: string | null }) {
  const hydrating = useSyncExternalStore(
    noopSubscribe,
    () => false,
    () => true,
  );

  return (
    <time dateTime={value ?? undefined} suppressHydrationWarning>
      {fmtDateTime(value, { utc: hydrating })}
    </time>
  );
}
