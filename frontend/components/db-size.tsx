import { fmtBytes } from "@/lib/utils";

/** Примерный размер базы из discovery под её именем; нет данных — ничего. */
export function DbSize({ bytes }: { bytes?: number | null }) {
  if (bytes == null) return null;
  return (
    <div
      className="text-xs whitespace-nowrap text-muted-foreground tabular-nums"
      title="Примерный размер базы (pg_database_size) по последнему обновлению списка баз"
    >
      ≈ {fmtBytes(bytes)}
    </div>
  );
}
