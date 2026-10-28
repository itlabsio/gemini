"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Database,
  FolderKanban,
  GitCompareArrows,
  Link2,
  ScrollText,
  Settings,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { HeadRole } from "@/lib/types";

interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
  show?: (ctx: { headRole: HeadRole; superuser: boolean }) => boolean;
}

const ITEMS: NavItem[] = [
  { href: "/instances", label: "Инстансы", icon: Database },
  { href: "/runs", label: "Журнал", icon: ScrollText },
  { href: "/pairing", label: "Pairing", icon: Link2, show: ({ superuser }) => superuser },
  { href: "/projects", label: "Проекты", icon: FolderKanban, show: ({ superuser }) => superuser },
  {
    href: "/mapping",
    label: "Сопоставление",
    icon: GitCompareArrows,
    show: ({ headRole }) => headRole === "target",
  },
  {
    href: "/settings",
    label: "Настройки",
    icon: Settings,
    show: ({ superuser }) => superuser,
  },
];

export function Nav({ headRole, superuser }: { headRole: HeadRole; superuser: boolean }) {
  const pathname = usePathname();

  return (
    <nav className="flex w-max items-center gap-1">
      {ITEMS.filter((i) => !i.show || i.show({ headRole, superuser })).map((item) => {
        const active =
          pathname === item.href || pathname.startsWith(`${item.href}/`);
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "inline-flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm font-medium transition-colors",
              "focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50",
              active
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:bg-secondary/60 hover:text-foreground",
            )}
          >
            <Icon className="size-4 shrink-0" />
            <span className="hidden sm:inline">{item.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
