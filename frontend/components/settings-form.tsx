"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import type { HeadRole, Settings } from "@/lib/types";
import { updateSettingsAction } from "@/app/settings/actions";
import { bytesQuantityError, MIN_STORAGE_SIZE } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
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

// Границы TTL завершённых Job'ов в минутах — как на бэкенде (5 минут … 7 суток)
const JOB_TTL_MIN = 5;
const JOB_TTL_MAX = 7 * 24 * 60;
// Таймаут старта пода в минутах — как на бэкенде (1 минута … сутки)
const POD_START_MIN = 1;
const POD_START_MAX = 24 * 60;
// Сроки хранения аудита и истории прогонов в днях — как на бэкенде (2 недели … 10 лет)
const RETENTION_MIN = 14;
const RETENTION_MAX = 3650;

// Срок жизни ссылки на скачивание дампа, минуты — как CHECK в миграции 025 (потолок SigV4).
const DOWNLOAD_TTL_MIN = 1;
const DOWNLOAD_TTL_MAX = 7 * 24 * 60;

/** Целое число в [min, max] или текст ошибки (unit — «минут», «дней»). */
function intError(v: string, min: number, max: number, unit = "минут"): string | null {
  const n = Number(v.trim());
  if (!v.trim() || !Number.isInteger(n) || n < min || n > max) {
    return `Целое число ${unit} от ${min} до ${max}`;
  }
  return null;
}

export function SettingsForm({ initial, headRole }: { initial: Settings; headRole: HeadRole }) {
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
  const chartRes = initial.chart_resources ?? {};
  // Подсказка — планирование из values чарта: оно действует, пока поле пусто.
  const schedPlaceholder = initial.chart_pod_scheduling
    ? JSON.stringify(initial.chart_pod_scheduling, null, 2)
    : '{\n  "nodeSelector": { "node-group": "backup" }\n}';
  const [ttl, setTtl] = useState(String(initial.job_ttl_minutes));
  const [podStart, setPodStart] = useState(
    String(initial.job_pod_start_timeout_minutes),
  );
  const [onFailure, setOnFailure] = useState(initial.notify_on_failure);
  const [onSuccess, setOnSuccess] = useState(initial.notify_on_success);
  const [auditDays, setAuditDays] = useState(String(initial.audit_retention_days));
  const [runsDays, setRunsDays] = useState(String(initial.runs_retention_days));
  const [downloadTtl, setDownloadTtl] = useState(String(initial.download_url_ttl_minutes));

  const sizeErr = size.trim()
    ? bytesQuantityError(size, { min: MIN_STORAGE_SIZE })
    : "Обязательное поле";
  const reqMemErr = bytesQuantityError(reqMem);
  const limMemErr = bytesQuantityError(limMem);
  const ttlErr = intError(ttl, JOB_TTL_MIN, JOB_TTL_MAX);
  const podStartErr = intError(podStart, POD_START_MIN, POD_START_MAX);
  const auditDaysErr = intError(auditDays, RETENTION_MIN, RETENTION_MAX, "дней");
  const runsDaysErr = intError(runsDays, RETENTION_MIN, RETENTION_MAX, "дней");
  const downloadTtlErr = intError(downloadTtl, DOWNLOAD_TTL_MIN, DOWNLOAD_TTL_MAX);
  const invalid = Boolean(
    sizeErr || reqMemErr || limMemErr || ttlErr || podStartErr || auditDaysErr || runsDaysErr ||
      downloadTtlErr,
  );

  const dirty =
    type !== initial.default_storage_type ||
    size !== initial.default_storage_size ||
    reqCpu !== (initial.default_resources?.requests?.cpu ?? "") ||
    reqMem !== (initial.default_resources?.requests?.memory ?? "") ||
    limCpu !== (initial.default_resources?.limits?.cpu ?? "") ||
    limMem !== (initial.default_resources?.limits?.memory ?? "") ||
    sched.trim() !== initialSched.trim() ||
    ttl.trim() !== String(initial.job_ttl_minutes) ||
    podStart.trim() !== String(initial.job_pod_start_timeout_minutes) ||
    onFailure !== initial.notify_on_failure ||
    onSuccess !== initial.notify_on_success ||
    auditDays.trim() !== String(initial.audit_retention_days) ||
    runsDays.trim() !== String(initial.runs_retention_days) ||
    downloadTtl.trim() !== String(initial.download_url_ttl_minutes);

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

    if (invalid) return;

    start(async () => {
      const res = await updateSettingsAction({
        default_storage_type: type,
        default_storage_size: size,
        default_resources: {
          requests: { cpu: reqCpu || undefined, memory: reqMem || undefined },
          limits: { cpu: limCpu || undefined, memory: limMem || undefined },
        },
        default_pod_scheduling: scheduling,
        job_ttl_minutes: Number(ttl.trim()),
        job_pod_start_timeout_minutes: Number(podStart.trim()),
        notify_on_failure: onFailure,
        notify_on_success: onSuccess,
        audit_retention_days: Number(auditDays.trim()),
        runs_retention_days: Number(runsDays.trim()),
        download_url_ttl_minutes: Number(downloadTtl.trim()),
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
          <Field
            label="Размер"
            htmlFor="storage_size"
            error={sizeErr}
            className="flex-1"
          >
            <Input
              id="storage_size"
              aria-invalid={Boolean(sizeErr)}
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
            Заполненные поля перекрывают ресурсы из values чарта
            (job.resources), а без них — встроенные дефолты; пустое поле берёт
            значение оттуда, оно — в подсказке
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
              placeholder={chartRes.requests?.cpu ?? ""}
            />
            <Input
              aria-invalid={Boolean(reqMemErr)}
              value={reqMem}
              onChange={(e) => setReqMem(e.target.value)}
              placeholder={chartRes.requests?.memory ?? ""}
            />

            <span className="text-sm font-medium">limits</span>
            <Input
              value={limCpu}
              onChange={(e) => setLimCpu(e.target.value)}
              placeholder={chartRes.limits?.cpu ?? ""}
            />
            <Input
              aria-invalid={Boolean(limMemErr)}
              value={limMem}
              onChange={(e) => setLimMem(e.target.value)}
              placeholder={chartRes.limits?.memory ?? ""}
            />
          </div>
          {(reqMemErr || limMemErr) && (
            <p className="mt-2 text-xs text-destructive">
              Memory: {reqMemErr ?? limMemErr}
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Планирование dump/restore-подов</CardTitle>
          <CardDescription>
            JSON с полями nodeSelector / tolerations / affinity (как в PodSpec).
            Перекрывает значения из values чарта (job.*).{" "}
            {initial.chart_pod_scheduling
              ? "Пусто → значения чарта (в подсказке)"
              : "Пусто → значения чарта (в чарте не заданы)"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Textarea
            value={sched}
            onChange={(e) => setSched(e.target.value)}
            rows={Math.min(Math.max(8, schedPlaceholder.split("\n").length), 24)}
            spellCheck={false}
            placeholder={schedPlaceholder}
            className="font-mono text-xs"
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Таймауты dump/restore-Job’ов</CardTitle>
          <CardDescription>
            Ожидание старта: если за это время под Job’а так и не запустился
            (не создаётся, висит в Pending), Job удаляется, а прогон
            закрывается с ошибкой. TTL: через сколько минут после завершения
            k8s удаляет Job вместе с подами и их логами — применяется к
            CronJob’ам при следующей сверке шаблонов (до ~6 минут)
          </CardDescription>
        </CardHeader>
        <CardContent className="flex gap-4">
          <Field
            label="Ожидание старта пода, мин"
            htmlFor="job_pod_start_timeout_minutes"
            error={podStartErr}
            className="flex-1"
          >
            <Input
              id="job_pod_start_timeout_minutes"
              aria-invalid={Boolean(podStartErr)}
              type="number"
              inputMode="numeric"
              min={POD_START_MIN}
              max={POD_START_MAX}
              step={1}
              value={podStart}
              onChange={(e) => setPodStart(e.target.value)}
              placeholder="10"
            />
          </Field>
          <Field
            label="TTL завершённых, мин"
            htmlFor="job_ttl_minutes"
            error={ttlErr}
            className="flex-1"
          >
            <Input
              id="job_ttl_minutes"
              aria-invalid={Boolean(ttlErr)}
              type="number"
              inputMode="numeric"
              min={JOB_TTL_MIN}
              max={JOB_TTL_MAX}
              step={1}
              value={ttl}
              onChange={(e) => setTtl(e.target.value)}
              placeholder="5"
            />
          </Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Уведомления</CardTitle>
          <CardDescription>
            О каких исходах dump/restore сообщать. Канал (Slack / Telegram)
            задаётся в чарте (notify.*); если он не включён, уведомления не
            отправляются
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <Switch
              id="notify_on_failure"
              checked={onFailure}
              onCheckedChange={setOnFailure}
            />
            <Label htmlFor="notify_on_failure">Провалы</Label>
          </div>
          <div className="flex items-center gap-2">
            <Switch
              id="notify_on_success"
              checked={onSuccess}
              onCheckedChange={setOnSuccess}
            />
            <Label htmlFor="notify_on_success">Успешные прогоны</Label>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Хранение</CardTitle>
          <CardDescription>
            Через сколько дней удалять записи журнала аудита и завершённые
            прогоны. Активные прогоны и последний прогон каждого вида по базе
            не удаляются никогда — на них держится защита от повторного
            restore. Очистка — раз в час
          </CardDescription>
        </CardHeader>
        <CardContent className="flex gap-4">
          <Field
            label="Журнал аудита, дней"
            htmlFor="audit_retention_days"
            error={auditDaysErr}
            className="flex-1"
          >
            <Input
              id="audit_retention_days"
              aria-invalid={Boolean(auditDaysErr)}
              type="number"
              inputMode="numeric"
              min={RETENTION_MIN}
              max={RETENTION_MAX}
              step={1}
              value={auditDays}
              onChange={(e) => setAuditDays(e.target.value)}
              placeholder="365"
            />
          </Field>
          <Field
            label="История прогонов, дней"
            htmlFor="runs_retention_days"
            error={runsDaysErr}
            className="flex-1"
          >
            <Input
              id="runs_retention_days"
              aria-invalid={Boolean(runsDaysErr)}
              type="number"
              inputMode="numeric"
              min={RETENTION_MIN}
              max={RETENTION_MAX}
              step={1}
              value={runsDays}
              onChange={(e) => setRunsDays(e.target.value)}
              placeholder="90"
            />
          </Field>
        </CardContent>
      </Card>

      {headRole === "source" && (
        <Card>
          <CardHeader>
            <CardTitle>Скачивание дампов</CardTitle>
            <CardDescription>
              Сколько живёт ссылка на скачивание дампа. Отозвать выданную ссылку
              нельзя — до истечения срока по ней скачает любой, у кого она есть
            </CardDescription>
          </CardHeader>
          <CardContent className="flex gap-4">
            <Field
              label="Срок жизни ссылки, минут"
              htmlFor="download_url_ttl_minutes"
              error={downloadTtlErr}
              className="flex-1"
            >
              <Input
                id="download_url_ttl_minutes"
                aria-invalid={Boolean(downloadTtlErr)}
                type="number"
                inputMode="numeric"
                min={DOWNLOAD_TTL_MIN}
                max={DOWNLOAD_TTL_MAX}
                step={1}
                value={downloadTtl}
                onChange={(e) => setDownloadTtl(e.target.value)}
                placeholder="15"
              />
            </Field>
          </CardContent>
        </Card>
      )}

      <div className="flex items-center gap-3">
        <Button onClick={save} disabled={pending || !dirty || invalid}>
          {pending ? "Сохранение…" : "Сохранить"}
        </Button>
        {!dirty && (
          <span className="text-sm text-muted-foreground">Изменений нет</span>
        )}
      </div>
    </div>
  );
}
