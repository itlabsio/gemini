import { cn } from "@/lib/utils";

/**
 * Усекает длинную строку по центру: `c-abc…yandexcloud.net:6432`.
 * Голова сжимается с многоточием, хвост (порт, имя секрета) виден всегда.
 * Родитель должен ограничивать ширину — компонент занимает не больше неё.
 */
export function MiddleTruncate({
  text,
  tail = 12,
  className,
}: {
  text: string;
  tail?: number;
  className?: string;
}) {
  const split = text.length > tail + 1 ? text.length - tail : 0;
  const head = split ? text.slice(0, split) : text;
  const rest = split ? text.slice(split) : "";
  return (
    <span className={cn("flex max-w-full min-w-0", className)} title={text}>
      <span className="truncate">{head}</span>
      {rest && <span className="flex-none">{rest}</span>}
    </span>
  );
}
