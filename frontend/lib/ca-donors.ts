import type { Instance } from "@/lib/types";

export type CaDonor = { id: string; name: string; ssl_root_cert: string };

/**
 * Инстансы с кастомным CA — источники для копирования PEM в форме инстанса.
 * `exceptId` исключает редактируемый инстанс (копировать у себя незачем).
 */
export function caDonorsFrom(instances: Instance[], exceptId?: string): CaDonor[] {
  return instances
    .filter((i) => i.id !== exceptId && !!i.ssl_root_cert)
    .map((i) => ({ id: i.id, name: i.name, ssl_root_cert: i.ssl_root_cert! }));
}
