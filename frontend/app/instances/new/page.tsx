import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { getMe, listInstances, listProjects } from "@/lib/api";
import { InstanceForm } from "@/components/instance-form";
import { caDonorsFrom } from "@/lib/ca-donors";

export const metadata: Metadata = { title: "Новый инстанс" };

export default async function NewInstancePage() {
  const me = await getMe();
  if (!me.superuser) notFound();
  const [instances, projects] = await Promise.all([
    listInstances().catch(() => []),
    listProjects().catch(() => []),
  ]);
  return (
    <InstanceForm
      vaultEnabled={me.vault_enabled}
      headRole={me.head_role}
      caDonors={caDonorsFrom(instances)}
      projects={me.superuser ? projects.map((p) => p.name) : undefined}
    />
  );
}
