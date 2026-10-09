"use client";

import { useEffect, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Check, Copy, KeyRound, Link2 } from "lucide-react";
import { issuePairingCodeAction, initPairingAction } from "@/app/pairing/actions";
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
      <CardContent>
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
