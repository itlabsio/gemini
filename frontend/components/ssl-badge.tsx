import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// disable — оранжевый (небезопасно), require — зелёный,
// verify-ca — бирюзовый, verify-full — голубой.
const STYLES: Record<string, string> = {
  disable: "bg-orange-500/12 text-orange-600 dark:text-orange-400",
  require: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400",
  "verify-ca": "bg-teal-500/12 text-teal-600 dark:text-teal-400",
  "verify-full": "bg-sky-500/12 text-sky-600 dark:text-sky-400",
};

export function SslModeBadge({ mode }: { mode: string }) {
  return (
    <Badge
      variant="outline"
      className={cn(
        "border-transparent font-mono",
        STYLES[mode] ?? "bg-muted text-muted-foreground",
      )}
    >
      {mode}
    </Badge>
  );
}
