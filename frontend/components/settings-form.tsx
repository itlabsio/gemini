"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import type { Settings } from "@/lib/types";
import { updateSettingsAction } from "@/app/settings/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/ui/field";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export function SettingsForm({ initial }: { initial: Settings }) {
  const [pending, start] = useTransition();

  const [type, setType] = useState(initial.default_storage_type);
  const [size, setSize] = useState(initial.default_storage_size);
  const [reqCpu, setReqCpu] = useState(
    initial.default_resources?.requests?.cpu ?? "",
  );
  const [reqMem, setReqMem] = useState(
    initial.default_resources?.requests?.memory ?? "",
  );
  const [limCpu, setLimCpu] = useState(
    initial.default_resources?.limits?.cpu ?? "",
  );
  const [limMem, setLimMem] = useState(
    initial.default_resources?.limits?.memory ?? "",
  );
  const initialSched = initial.default_pod_scheduling
    ? JSON.stringify(initial.default_pod_scheduling, null, 2)
    : "";
  const [sched, setSched] = useState(initialSched);

  const dirty =
    type !== initial.default_storage_type ||
    size !== initial.default_storage_size ||
    reqCpu !== (initial.default_resources?.requests?.cpu ?? "") ||
    reqMem !== (initial.default_resources?.requests?.memory ?? "") ||
    limCpu !== (initial.default_resources?.limits?.cpu ?? "") ||
    limMem !== (initial.default_resources?.limits?.memory ?? "") ||
    sched.trim() !== initialSched.trim();

  function save() {
    let scheduling: Record<string, unknown> | null = null;
    if (sched.trim()) {
      try {
        scheduling = JSON.parse(sched);
      } catch {
        toast.error("Планирование подов: невалидный JSON");
        return;
      }
    }

    start(async () => {
      const res = await updateSettingsAction({
        default_storage_type: type,
        default_storage_size: size,
        default_resources: {
          requests: { cpu: reqCpu || undefined, memory: reqMem || undefined },
          limits: { cpu: limCpu || undefined, memory: limMem || undefined },
        },
        default_pod_scheduling: scheduling,
      });
      if (res.ok) toast.success("Сохранено");
      else toast.error(res.error);
    });
  }

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Временное хранилище дампа</CardTitle>
          <CardDescription>
            Применяется к базам, у которых тип/размер не заданы индивидуально
          </CardDescription>
        </CardHeader>
        <CardContent className="flex gap-4">
          <Field label="Тип" className="flex-1">
            <Select
              value={type}
              onValueChange={(v) =>
                setType(v as Settings["default_storage_type"])
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="ephemeral">ephemeral PVC</SelectItem>
                <SelectItem value="emptydir">emptyDir</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="Размер" htmlFor="storage_size" className="flex-1">
            <Input
              id="storage_size"
              value={size}
              onChange={(e) => setSize(e.target.value)}
              placeholder="20Gi"
            />
          </Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Ресурсы dump/restore-подов</CardTitle>
          <CardDescription>
            Пустые поля → встроенные дефолты бэкенда (0.5 CPU / 512Mi … 2 CPU /
            2Gi)
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-[auto_1fr_1fr] items-center gap-3">
            <span />
            <span className="text-xs text-muted-foreground">CPU</span>
            <span className="text-xs text-muted-foreground">Memory</span>

            <span className="text-sm font-medium">requests</span>
            <Input
              value={reqCpu}
              onChange={(e) => setReqCpu(e.target.value)}
              placeholder="500m"
            />
            <Input
              value={reqMem}
              onChange={(e) => setReqMem(e.target.value)}
              placeholder="512Mi"
            />

            <span className="text-sm font-medium">limits</span>
            <Input
              value={limCpu}
              onChange={(e) => setLimCpu(e.target.value)}
              placeholder="2"
            />
            <Input
              value={limMem}
              onChange={(e) => setLimMem(e.target.value)}
              placeholder="2Gi"
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Планирование dump/restore-подов</CardTitle>
          <CardDescription>
            JSON с полями nodeSelector / tolerations / affinity (как в PodSpec).
            Перекрывает значения из values чарта (job.*). Пусто → значения чарта
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Textarea
            value={sched}
            onChange={(e) => setSched(e.target.value)}
            rows={8}
            spellCheck={false}
            placeholder={'{\n  "nodeSelector": { "node-group": "backup" }\n}'}
            className="font-mono text-xs"
          />
        </CardContent>
      </Card>

      <div className="flex items-center gap-3">
        <Button onClick={save} disabled={pending || !dirty}>
          {pending ? "Сохранение…" : "Сохранить"}
        </Button>
        {!dirty && (
          <span className="text-sm text-muted-foreground">Изменений нет</span>
        )}
      </div>
    </div>
  );
}
