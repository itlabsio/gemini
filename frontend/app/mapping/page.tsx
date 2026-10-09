import Link from "next/link";
import { redirect } from "next/navigation";
import {
  getMe,
  listDatabases,
  listInstances,
  listMappings,
  listPairings,
} from "@/lib/api";
import type { DatabaseMapping } from "@/lib/types";
import { MappingEditor } from "@/components/mapping-editor";
import { PageHeader } from "@/components/ui/page-header";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export default async function MappingPage() {
  const me = await getMe();
  if (me.head_role !== "target") redirect("/instances");

  const instances = await listInstances().catch(() => []);
  const perInstance = await Promise.all(
    instances.map((i) =>
      listDatabases(i.id)
        .then((dbs) =>
          dbs.map((d) => ({
            id: d.id,
            instanceId: i.id,
            label: `${i.name}/${d.db_name}`,
          })),
        )
        .catch(() => []),
    ),
  );
  const targets = perInstance.flat();

  let existing: DatabaseMapping[] = [];
  let error: string | null = null;
  try {
    existing = await listMappings();
  } catch (e) {
    error = e instanceof Error ? e.message : "Ошибка загрузки";
  }

  const pairings = await listPairings().catch(() => []);
  const hasSourcePairing = pairings.some(
    (p) => p.peer_role === "source" && p.status === "active",
  );
  const targetLabel = new Map(targets.map((t) => [t.id, t.label]));

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Сопоставление баз"
        description="Для каждой source-базы выберите целевую базу в Target-контуре. Префикс дампа в S3 подставляется из Source"
      />

      <Alert variant="info">
        <p className="font-medium">Порядок настройки Target</p>
        <ol className="ml-4 list-decimal space-y-0.5">
          <li>
            Установить пару с Source на странице{" "}
            <Link href="/pairing" className="underline underline-offset-2">
              Pairing
            </Link>{" "}
            {hasSourcePairing ? (
              <Badge variant="success">готово</Badge>
            ) : (
              <Badge variant="secondary">не сделано</Badge>
            )}
          </li>
          <li>«Загрузить список баз с Source» — кнопка ниже</li>
          <li>Для каждой базы выбрать целевую и сохранить</li>
        </ol>
      </Alert>

      {error && <Alert variant="destructive">{error}</Alert>}

      {me.role !== "admin" ? (
        <Alert variant="info">
          Редактирование сопоставлений доступно роли admin
        </Alert>
      ) : !hasSourcePairing ? (
        <Alert variant="destructive">
          Нет активной пары с Source. Сначала пройдите{" "}
          <Link href="/pairing" className="underline underline-offset-2">
            pairing
          </Link>
          , затем вернитесь сюда за списком баз.
        </Alert>
      ) : (
        <MappingEditor targets={targets} existing={existing} />
      )}

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Текущие сопоставления</h2>
        <div className="rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Source база</TableHead>
                <TableHead>Целевая база</TableHead>
                <TableHead>Владелец</TableHead>
                <TableHead>S3 prefix</TableHead>
                <TableHead>Активно</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {existing.map((m) => (
                <TableRow key={m.id}>
                  <TableCell className="font-medium">
                    {m.source_db_name || m.source_database_external_id}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {targetLabel.get(m.target_database_id) ?? m.target_database_id}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {m.target_owner || "—"}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {m.s3_prefix}
                  </TableCell>
                  <TableCell>
                    <Badge variant={m.enabled ? "success" : "secondary"}>
                      {m.enabled ? "да" : "нет"}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
              {existing.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="py-10 text-center text-muted-foreground"
                  >
                    Пусто
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </section>
    </div>
  );
}
