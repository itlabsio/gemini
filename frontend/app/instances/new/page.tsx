import { getMe, listInstances } from "@/lib/api";
import { InstanceForm } from "@/components/instance-form";
import { caDonorsFrom } from "@/lib/ca-donors";

export default async function NewInstancePage() {
  const [me, instances] = await Promise.all([
    getMe(),
    listInstances().catch(() => []),
  ]);
  return (
    <InstanceForm
      vaultEnabled={me.vault_enabled}
      headRole={me.head_role}
      caDonors={caDonorsFrom(instances)}
    />
  );
}
