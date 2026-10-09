import { notFound } from "next/navigation";
import { ApiError, getInstance, getMe, listInstances } from "@/lib/api";
import { InstanceForm } from "@/components/instance-form";
import { caDonorsFrom } from "@/lib/ca-donors";

interface Props {
  params: Promise<{ id: string }>;
}

export default async function EditInstancePage({ params }: Props) {
  const { id } = await params;

  const me = await getMe();
  if (me.role !== "admin") notFound();

  let instance;
  try {
    instance = await getInstance(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const instances = await listInstances().catch(() => []);

  return (
    <InstanceForm
      initial={instance}
      vaultEnabled={me.vault_enabled}
      headRole={me.head_role}
      caDonors={caDonorsFrom(instances, id)}
    />
  );
}
