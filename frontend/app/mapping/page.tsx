import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import {
  getMe,
  listDatabases,
  listInstances,
  listMappings,
} from "@/lib/api";
import { can, projectsAtLeast } from "@/lib/rbac";
import type { DatabaseMapping } from "@/lib/types";
import { MappingEditor } from "@/components/mapping-editor";
import { MappingActiveBadge } from "@/components/mapping-active-badge";
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

export const metadata: Metadata = { title: "Сопоставление" };

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
            project: i.project,
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

  // Проекты, в которых можно сопоставлять: admin проекта (superuser — любой
  // проект с target-инстансами). Инстансы без проекта сопоставлять нельзя.
  const instanceProjects = [
    ...new Set(instances.map((i) => i.project).filter(Boolean)),
  ].sort();
  const editableProjects = me.superuser
    ? instanceProjects
    : projectsAtLeast(me, "admin").filter((p) => instanceProjects.includes(p));
  const hasSourcePairing = me.source_paired;
  const targetLabel = new Map(targets.map((t) => [t.id, t.label]));
  const targetProject = new Map(targets.map((t) => [t.id, t.project]));

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

      {editableProjects.length === 0 ? (
        <Alert variant="info">
          Редактирование сопоставлений доступно admin проекта (и superuser), если у
          проекта есть target-инстансы
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
        <MappingEditor
          projects={editableProjects}
          targets={targets}
          existing={existing}
        />
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
                    <MappingActiveBadge
                      mapping={m}
                      sourceLabel={m.source_db_name || m.source_database_external_id}
                      targetLabel={
                        targetLabel.get(m.target_database_id) ?? m.target_database_id
                      }
                      canUnlink={can(
                        me,
                        targetProject.get(m.target_database_id),
                        "admin",
                      )}
                    />
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
