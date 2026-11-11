"use client";

import { useEffect, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Check, Copy, KeyRound, Link2 } from "lucide-react";
import {
  issuePairingCodeAction,
  initOfflinePairingAction,
  initPairingAction,
} from "@/app/pairing/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const CODE_COOLDOWN_MS = 60_000;

export function IssueCodePanel({ lastIssuedAt }: { lastIssuedAt?: string }) {
  const [pending, start] = useTransition();
  const [code, setCode] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  // Локально отмеченный момент выдачи (после нажатия/429). Итоговый момент —
  // максимум из него и значения с сервера, считается в рендере без эффекта.
  const [localIssuedMs, setLocalIssuedMs] = useState(0);
  const [now, setNow] = useState(() => Date.now());

  const propIssuedMs = lastIssuedAt ? new Date(lastIssuedAt).getTime() : 0;
  const issuedAt = Math.max(propIssuedMs, localIssuedMs);

  const remainingMs = issuedAt
    ? Math.max(0, issuedAt + CODE_COOLDOWN_MS - now)
    : 0;
  const remaining = Math.ceil(remainingMs / 1000);

  useEffect(() => {
    if (remainingMs <= 0) return;
    const id = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(id);
  }, [remainingMs]);

  function copy() {
    if (!code) return;
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  const blocked = pending || remaining > 0;

  return (
    <Card className="max-w-lg">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4" />
          Выдать код для Target
        </CardTitle>
        <CardDescription>
          Код одноразовый, живёт 15 минут. Передайте его вместе с URL
          Source-контура администратору Target-контура
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Button
          className="self-start"
          onClick={() =>
            start(async () => {
              const res = await issuePairingCodeAction();
              if (res.ok) {
                setCode(res.code);
                setLocalIssuedMs(Date.now());
              } else {
                if (res.retryAfter) {
                  setLocalIssuedMs(
                    Date.now() - (CODE_COOLDOWN_MS - res.retryAfter * 1000),
                  );
                }
                toast.error(res.error);
              }
            })
          }
          disabled={blocked}
        >
          {pending
            ? "…"
            : remaining > 0
              ? `Создать новый через ${remaining} с`
              : "Сгенерировать код"}
        </Button>

        {code && (
          <div className="flex items-center gap-2 rounded-md border bg-muted/40 p-3">
            <code className="flex-1 font-mono text-sm break-all">{code}</code>
            <Button
              variant="outline"
              size="icon"
              onClick={copy}
              aria-label="Копировать"
            >
              {copied ? <Check /> : <Copy />}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function InitPairingPanel() {
  const [pending, start] = useTransition();
  const [ok, setOk] = useState(false);
  const router = useRouter();

  function onSubmit(fd: FormData) {
    setOk(false);
    start(async () => {
      const res = await initPairingAction(
        String(fd.get("source_url") ?? ""),
        String(fd.get("code") ?? ""),
      );
      if (res.ok) {
        setOk(true);
        toast.success("Пара установлена");
        router.refresh(); // подтянуть новую строку в таблице связей без F5
      } else {
        toast.error(res.error);
      }
    });
  }

  return (
    <Card className="max-w-lg">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Link2 className="size-4" />
          Инициализация пары с Source
        </CardTitle>
        <CardDescription>
          Введите URL Source-контура и одноразовый код, выданный его администратором
        </CardDescription>
      </CardHeader>
      <CardContent className="mt-auto">
        <form action={onSubmit} className="flex flex-col gap-4">
          <Field label="URL Source-контура" htmlFor="source_url">
            <Input
              id="source_url"
              name="source_url"
              required
              placeholder="https://gemini.source.example"
            />
          </Field>
          <Field label="Код авторизации" htmlFor="code">
            <Input id="code" name="code" required className="font-mono" />
          </Field>
          <Button type="submit" className="self-start" disabled={pending}>
            {pending ? "Обмен…" : "Установить доверие"}
          </Button>
          {ok && <p className="text-sm text-success">Пара установлена</p>}
        </form>
      </CardContent>
    </Card>
  );
}

/**
 * Оффлайн-пара: Source недоступен (или его нет), но его публичный ключ сохранён
 * заранее. Дампы и сопоставления берутся из бакета, подписи проверяются этим ключом.
 */
export function OfflinePairingPanel() {
  const [pending, start] = useTransition();
  const router = useRouter();

  function onSubmit(fd: FormData) {
    start(async () => {
      const res = await initOfflinePairingAction(String(fd.get("public_key") ?? ""));
      if (res.ok) {
        toast.success("Ключ Source запомнен");
        router.refresh();
      } else {
        toast.error(res.error);
      }
    });
  }

  return (
    <Card className="max-w-lg">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4" />
          Оффлайн-пара по ключу Source
        </CardTitle>
        <CardDescription>
          Если Source недоступен: вставьте его публичный ключ, сохранённый заранее
          (Source показывает его на этой странице). Дампы и базы для сопоставления
          будут найдены в бакете, каждый restore проверяется подписью этим ключом.
          Сверьте ключ с сохранённой копией символ в символ — от него зависит,
          каким дампам Target поверит.
        </CardDescription>
      </CardHeader>
      <CardContent className="mt-auto">
        <form action={onSubmit} className="flex flex-col gap-4">
          <Field label="Публичный ключ Source (base64)" htmlFor="public_key">
            <Input id="public_key" name="public_key" required className="font-mono" />
          </Field>
          <Button type="submit" variant="outline" className="self-start" disabled={pending}>
            {pending ? "Сохранение…" : "Запомнить ключ"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
