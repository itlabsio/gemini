import { KeyRound, Lock } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { AuthType } from "@/lib/types";

// plain (логин/пароль хранит сам Gemini) — жёлтая подложка,
// vault (секрет живёт в Vault) — зелёная.
export function SecretBadge({ authType }: { authType: AuthType }) {
  const vault = authType === "vault";
  const Icon = vault ? KeyRound : Lock;
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1 border-transparent",
        vault
          ? "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400"
          : "bg-yellow-400/15 text-yellow-700 dark:text-yellow-400",
      )}
    >
      <Icon />
      {authType}
    </Badge>
  );
}
