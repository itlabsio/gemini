import {
  CircleCheck,
  CircleDashed,
  CircleX,
  LoaderCircle,
  type LucideIcon,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";

type BadgeVariant = "success" | "destructive" | "info" | "warning" | "secondary";

const MAP: Record<string, { variant: BadgeVariant; icon: LucideIcon }> = {
  succeeded: { variant: "success", icon: CircleCheck },
  active: { variant: "success", icon: CircleCheck },
  failed: { variant: "destructive", icon: CircleX },
  revoked: { variant: "destructive", icon: CircleX },
  running: { variant: "info", icon: LoaderCircle },
  pending: { variant: "warning", icon: CircleDashed },
};

/** Бейдж статуса прогона / связи / пира с иконкой и цветом. */
export function StatusBadge({ status }: { status?: string | null }) {
  if (!status) return <span className="text-muted-foreground">—</span>;
  const cfg = MAP[status] ?? { variant: "secondary" as const, icon: CircleDashed };
  const Icon = cfg.icon;
  return (
    <Badge variant={cfg.variant} className="gap-1">
      <Icon className={status === "running" ? "animate-spin" : undefined} />
      {status}
    </Badge>
  );
}
