import { Lock } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { AuthType } from "@/lib/types";

/**
 * Логомарк HashiCorp Vault из официального брендбука (Vault-LogoMark), viewBox
 * обрезан по треугольнику; товарный знак HashiCorp — используется только как
 * указатель «секрет в Vault». Цвет — currentColor бейджа.
 */
function VaultIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 10.2 53.2 51.6" aria-hidden="true" className={className} fill="currentColor">
      <path d="m0,10.2l26.5,51.6L53.2,10.2H0Zm22.4,20.5h-4.2v-4.2h4.2v4.2Zm0-6.3h-4.2v-4.2h4.2v4.2Zm6.3,12.6h-4.2v-4.2h4.2v4.2Zm0-6.3h-4.2v-4.2h4.2v4.2Zm0-6.3h-4.2v-4.2h4.2v4.2Zm6.3,6.3h-4.2v-4.2h4.2v4.2Zm-4.2-6.3v-4.2h4.2v4.2h-4.2Z" />
    </svg>
  );
}

// plain (логин/пароль хранит сам Gemini) — жёлтая подложка,
// vault (секрет живёт в Vault) — зелёная.
export function SecretBadge({ authType }: { authType: AuthType }) {
  const vault = authType === "vault";
  const Icon = vault ? VaultIcon : Lock;
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
