"use client";

import { useRouter } from "next/navigation";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// Значения Select не могут быть пустыми; имя проекта не содержит "/",
// поэтому служебные значения с ним не пересекаются.
const ALL = "/all";
const UNASSIGNED = "/none";

/** Фильтр списка инстансов по проекту: состояние живёт в URL (?project= / ?unassigned=1). */
export function InstancesProjectFilter({
  projects,
  value,
  unassigned,
  showUnassigned,
}: {
  projects: string[];
  value?: string;
  unassigned: boolean;
  /** Пункт «без проекта» — только если такие инстансы видны (superuser). */
  showUnassigned: boolean;
}) {
  const router = useRouter();
  const current = unassigned ? UNASSIGNED : (value ?? ALL);

  return (
    <Select
      value={current}
      onValueChange={(v) => {
        if (v === ALL) router.push("/instances");
        else if (v === UNASSIGNED) router.push("/instances?unassigned=1");
        else router.push(`/instances?project=${encodeURIComponent(v)}`);
      }}
    >
      <SelectTrigger size="sm" className="w-56" aria-label="Фильтр по проекту">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>Все проекты</SelectItem>
        {projects.map((p) => (
          <SelectItem key={p} value={p}>
            {p}
          </SelectItem>
        ))}
        {showUnassigned && <SelectItem value={UNASSIGNED}>Без проекта</SelectItem>}
      </SelectContent>
    </Select>
  );
}
