import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { getMe, getSettings, listNotificationChannels, listS3Buckets } from "@/lib/api";
import { LocalDateTime } from "@/components/local-date-time";
import { SettingsForm } from "@/components/settings-form";
import { S3BucketsManager } from "@/components/s3-buckets-manager";
import { NotificationChannels, SystemBots } from "@/components/notification-channels";
import { Alert } from "@/components/ui/alert";
import { PageHeader } from "@/components/ui/page-header";

export const metadata: Metadata = { title: "Настройки" };

export default async function SettingsPage() {
  const me = await getMe();
  if (!me.superuser) redirect("/instances");

  const [settings, buckets, notifications] = await Promise.all([
    getSettings(),
    listS3Buckets(),
    listNotificationChannels(""),
  ]);

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader
        title="Настройки"
        description="Дефолты для новых запусков: тип и размер временного диска дампа, ресурсы и планирование подов; уведомления; S3-бакеты дампов"
      />
      <p className="-mt-2 text-xs text-muted-foreground">
        Обновлено: <LocalDateTime value={settings.updated_at} />
        {settings.updated_by ? ` · ${settings.updated_by}` : ""}
      </p>
      <SettingsForm initial={settings} headRole={me.head_role} />
      <S3BucketsManager buckets={buckets} headRole={me.head_role} />
      {!notifications.channels.some((c) => c.enabled) && (
        <Alert variant="destructive">
          Нет включённых глобальных каналов: о провалах прогонов узнают только проекты со своими каналами
        </Alert>
      )}
      <SystemBots systemBots={notifications.system_bots} />
      <NotificationChannels
        project=""
        channels={notifications.channels}
        systemBots={notifications.system_bots}
      />
    </div>
  );
}
