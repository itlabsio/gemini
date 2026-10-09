"use client";

import { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { toast } from "sonner";
import { CloudDownload } from "lucide-react";
import type { Candidate, DatabaseMapping } from "@/lib/types";
import {
  loadCandidatesAction,
  loadRolesAction,
  saveMappingAction,
} from "@/app/mapping/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export interface TargetOption {
  id: string;
  instanceId: string;
  label: string;
}

type RolesState = Record<string, string[] | "loading" | "error">;

export function MappingEditor({
  targets,
  existing,
}: {
  targets: TargetOption[];
  existing: DatabaseMapping[];
}) {
  const [pending, start] = useTransition();
  const [pairingId, setPairingId] = useState<string | null>(null);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  // Кэш ролей по инстансу: грузим лениво, когда строка выбирает целевую базу.
  const [roles, setRoles] = useState<RolesState>({});
  const requested = useRef<Set<string>>(new Set());

  const existingBySource = new Map(
    existing.map((m) => [m.source_database_external_id, m]),
  );

  const loadRoles = useCallback((instanceId: string) => {
    if (!instanceId || requested.current.has(instanceId)) return;
    requested.current.add(instanceId);
    setRoles((r) => ({ ...r, [instanceId]: "loading" }));
    loadRolesAction(instanceId).then((res) => {
      setRoles((r) => ({ ...r, [instanceId]: res.ok ? res.roles : "error" }));
      if (!res.ok) toast.error(`Роли инстанса: ${res.error}`);
    });
  }, []);

  return (
    <div className="flex flex-col gap-4">
      <Button
        variant="outline"
        className="self-start"
        onClick={() =>
          start(async () => {
            const res = await loadCandidatesAction();
            if (res.ok) {
              setPairingId(res.pairingId);
              setCandidates(res.candidates);
            } else {
              toast.error(res.error);
            }
          })
        }
        disabled={pending}
      >
        <CloudDownload className={pending ? "animate-pulse" : undefined} />
        Загрузить список баз с Source
      </Button>

      {candidates.length > 0 && pairingId && (
        <div className="overflow-x-auto rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>База Source</TableHead>
                <TableHead>Целевая база (Target)</TableHead>
                <TableHead>Владелец</TableHead>
                <TableHead>S3 prefix</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {candidates.map((c) => (
                <MappingRow
                  key={c.external_id}
                  candidate={c}
                  pairingId={pairingId}
                  targets={targets}
                  roles={roles}
                  loadRoles={loadRoles}
                  existing={existingBySource.get(c.external_id)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function MappingRow({
  candidate,
  pairingId,
  targets,
  roles,
  loadRoles,
  existing,
}: {
  candidate: Candidate;
  pairingId: string;
  targets: TargetOption[];
  roles: RolesState;
  loadRoles: (instanceId: string) => void;
  existing?: DatabaseMapping;
}) {
  const [pending, start] = useTransition();
  const [target, setTarget] = useState(existing?.target_database_id ?? "");
  const [owner, setOwner] = useState(existing?.target_owner ?? "");
  // Префикс приходит от Source; администратор Target его обычно не трогает,
  // но поле оставлено редактируемым на случай нестандартной раскладки бакета.
  const [prefix, setPrefix] = useState(
    existing?.s3_prefix ?? candidate.s3_prefix ?? "",
  );

  const instanceId = targets.find((t) => t.id === target)?.instanceId ?? "";
  useEffect(() => {
    if (instanceId) loadRoles(instanceId);
  }, [instanceId, loadRoles]);

  const roleList = instanceId ? roles[instanceId] : undefined;
  const ownerValid = owner.trim() !== "";
  const canSave = !pending && !!target && ownerValid && prefix.trim() !== "";

  return (
    <TableRow>
      <TableCell className="font-medium">{candidate.db_name}</TableCell>
      <TableCell>
        <Select value={target} onValueChange={setTarget}>
          <SelectTrigger size="sm" className="w-56">
            <SelectValue placeholder="— выбрать —" />
          </SelectTrigger>
          <SelectContent>
            {targets.map((t) => (
              <SelectItem key={t.id} value={t.id}>
                {t.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TableCell>
      <TableCell>
        {Array.isArray(roleList) ? (
          <Select value={owner} onValueChange={setOwner}>
            <SelectTrigger
              size="sm"
              className="w-48"
              aria-invalid={!ownerValid}
            >
              <SelectValue placeholder="— роль —" />
            </SelectTrigger>
            <SelectContent>
              {roleList.map((r) => (
                <SelectItem key={r} value={r}>
                  {r}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <Input
            value={owner}
            onChange={(e) => setOwner(e.target.value)}
            placeholder={
              roleList === "loading" ? "загрузка ролей…" : "имя роли"
            }
            aria-invalid={!ownerValid}
            title="Заранее созданная роль на target-кластере, которой будет принадлежать восстановленная база"
            className="h-8 w-48 font-mono text-xs"
          />
        )}
      </TableCell>
      <TableCell>
        <Input
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          placeholder="prod-core/mydb"
          aria-invalid={prefix.trim() === ""}
          title="Подставлен из Source. Ограничивает, какие дампы можно применить в эту базу — меняйте только при нестандартной раскладке бакета"
          className="h-8 w-48 font-mono text-xs"
        />
      </TableCell>
      <TableCell className="text-right">
        <Button
          size="sm"
          disabled={!canSave}
          onClick={() =>
            start(async () => {
              const res = await saveMappingAction({
                head_pairing_id: pairingId,
                source_database_external_id: candidate.external_id,
                source_db_name: candidate.db_name,
                target_database_id: target,
                target_owner: owner.trim(),
                s3_prefix: prefix,
                enabled: true,
              });
              if (res.ok) toast.success(`${candidate.db_name}: сохранено`);
              else toast.error(res.error);
            })
          }
        >
          Сохранить
        </Button>
      </TableCell>
    </TableRow>
  );
}
