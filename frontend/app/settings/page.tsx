import { redirect } from "next/navigation";
import { getMe, getSettings, listS3Buckets } from "@/lib/api";
import { LocalDateTime } from "@/components/local-date-time";
import { SettingsForm } from "@/components/settings-form";
import { S3BucketsManager } from "@/components/s3-buckets-manager";
import { PageHeader } from "@/components/ui/page-header";

export default async function SettingsPage() {
  const me = await getMe();
  if (me.role !== "admin") redirect("/instances");

  const [settings, buckets] = await Promise.all([getSettings(), listS3Buckets()]);

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader
        title="Настройки"
        description="Дефолты для новых запусков: тип и размер временного диска дампа, ресурсы и планирование подов; S3-бакеты дампов"
      />
      <p className="-mt-2 text-xs text-muted-foreground">
        Обновлено: <LocalDateTime value={settings.updated_at} />
        {settings.updated_by ? ` · ${settings.updated_by}` : ""}
      </p>
      <SettingsForm initial={settings} />
      <S3BucketsManager buckets={buckets} />
    </div>
  );
}
