"use client";

import { signOut } from "next-auth/react";
import { LogOut, UserRound } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Badge } from "@/components/ui/badge";
import type { Me, ProjectRole } from "@/lib/types";

const ROLE_BADGE: Record<ProjectRole, { variant: "secondary" | "info"; className?: string }> = {
  admin: { variant: "secondary", className: "bg-violet-500/15 text-violet-500" },
  operator: { variant: "info" },
  viewer: { variant: "secondary", className: "bg-muted text-muted-foreground" },
};

export function UserMenu({
  email,
  superuser,
  security,
  projects,
}: {
  email: string;
  superuser: boolean;
  security: boolean;
  projects: Me["projects"];
}) {
  // У superuser полный доступ: роли в проектах ничего не добавляют.
  const rows = superuser ? [] : Object.entries(projects).sort(([a], [b]) => a.localeCompare(b));
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-2">
          <UserRound className="size-4" />
          <span className="hidden max-w-[16ch] truncate md:inline">{email}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72 max-w-[calc(100vw-2rem)]">
        <DropdownMenuLabel className="flex flex-col gap-2">
          <span className="truncate text-sm font-medium">{email}</span>
          {(superuser || security) && (
            <div className="flex flex-wrap gap-1.5">
              {superuser ? (
                <Badge variant="warning">superuser</Badge>
              ) : (
                <Badge variant="success">security</Badge>
              )}
            </div>
          )}
        </DropdownMenuLabel>
        {rows.length > 0 && (
          <ul className="flex max-h-64 flex-col gap-1.5 overflow-y-auto px-2 pb-2">
            {rows.map(([project, role]) => (
              <li key={project} className="flex items-center justify-between gap-3 text-sm">
                <span className="min-w-0 truncate" title={project}>
                  {project}
                </span>
                <Badge variant={ROLE_BADGE[role].variant} className={ROLE_BADGE[role].className}>
                  {role}
                </Badge>
              </li>
            ))}
          </ul>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onSelect={() => signOut({ callbackUrl: "/" })}
        >
          <LogOut className="size-4" />
          Выйти
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
