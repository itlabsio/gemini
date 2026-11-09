import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { getMe, listNotificationChannels } from "@/lib/api";
import { can } from "@/lib/rbac";
import { NotificationChannels } from "@/components/notification-channels";
import { PageHeader } from "@/components/ui/page-header";

export const metadata: Metadata = { title: "Уведомления проекта" };

interface Props {
  params: Promise<{ name: string }>;
}

export default async function ProjectNotificationsPage({ params }: Props) {
  const project = decodeURIComponent((await params).name);
  const me = await getMe();
  if (!can(me, project, "admin")) redirect("/instances");

  const { channels, system_bots } = await listNotificationChannels(project);

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <Link href="/projects" className="text-sm text-muted-foreground hover:underline">
        ← Проекты
      </Link>
      <PageHeader title={`Уведомления: ${project}`} description="Каналы для исходов dump и restore" />
      <NotificationChannels project={project} channels={channels} systemBots={system_bots} />
    </div>
  );
}
