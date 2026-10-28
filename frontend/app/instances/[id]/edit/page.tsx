import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { ApiError, getInstance, getMe, listInstances, listProjects } from "@/lib/api";
import { can } from "@/lib/rbac";
import { InstanceForm } from "@/components/instance-form";
import { caDonorsFrom } from "@/lib/ca-donors";

interface Props {
  params: Promise<{ id: string }>;
}

/** Заголовок вкладки — имя инстанса; без доступа или при ошибке — общее «Инстанс». */
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { id } = await params;
  try {
    return { title: (await getInstance(id)).name + " — правка" };
  } catch {
    return { title: "Инстанс" };
  }
}

export default async function EditInstancePage({ params }: Props) {
  const { id } = await params;

  const me = await getMe();

  let instance;
  try {
    instance = await getInstance(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  if (!can(me, instance.project, "admin")) notFound();

  const [instances, projects] = await Promise.all([
    listInstances().catch(() => []),
    me.superuser ? listProjects().catch(() => []) : Promise.resolve([]),
  ]);

  return (
    <InstanceForm
      initial={instance}
      vaultEnabled={me.vault_enabled}
      headRole={me.head_role}
      caDonors={caDonorsFrom(instances, id)}
      projects={me.superuser ? projects.map((p) => p.name) : undefined}
    />
  );
}
