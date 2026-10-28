import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

/**
 * Логотип Terraform (Simple Icons, CC0; товарный знак HashiCorp — используется
 * только как указатель «управляется Terraform»). Цвет — фирменный #844FBA.
 */
function TerraformIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      aria-hidden="true"
      className={className}
      fill="#844FBA"
    >
      <path d="M1.44 0v7.575l6.561 3.79V3.787zm21.12 4.227l-6.561 3.791v7.574l6.56-3.787zM8.72 4.23v7.575l6.561 3.787V8.018zm0 8.405v7.575L15.28 24v-7.578z" />
    </svg>
  );
}

/** Иконка ресурса под управлением Terraform (managed_by = "terraform"); пояснение — в подсказке. */
export function ManagedBadge({ managedBy }: { managedBy?: string }) {
  if (managedBy !== "terraform") return null;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex cursor-default" aria-label="Управляется Terraform">
          <TerraformIcon className="size-4" />
        </span>
      </TooltipTrigger>
      <TooltipContent>
        Ресурс управляется Terraform: изменения через UI перезапишет следующий
        terraform apply
      </TooltipContent>
    </Tooltip>
  );
}
