"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import { KeyRound } from "lucide-react";
import { vaultSecretKeysAction } from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Alert } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export interface KeyMapping {
  usernameKey: string;
  passwordKey: string;
}

/**
 * Поля секрета в Vault у всех называются по-разному, поэтому сопоставление
 * "какой ключ — логин, какой — пароль" задаётся вручную. Модалка читает у
 * бэкенда ИМЕНА полей по указанному пути (значения он не отдаёт) и предлагает
 * их выбрать; пустое сопоставление означает автоопределение по известным именам.
 */
export function VaultKeyMapper({
  path,
  value,
  onChange,
}: {
  path: string;
  value: KeyMapping;
  onChange: (m: KeyMapping) => void;
}) {
  const [open, setOpen] = useState(false);
  const [pending, start] = useTransition();
  const [keys, setKeys] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState<KeyMapping>(value);

  function load() {
    setError(null);
    setKeys([]);
    setOpen(true);
    start(async () => {
      const res = await vaultSecretKeysAction(path);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setKeys(res.keys);
      // Уже заданное сопоставление важнее догадки — не затираем выбор админа.
      setDraft({
        usernameKey: value.usernameKey || res.guessUsernameKey,
        passwordKey: value.passwordKey || res.guessPasswordKey,
      });
    });
  }

  const mapped = value.usernameKey && value.passwordKey;

  return (
    <>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={path.trim() === ""}
          title={
            path.trim() === "" ? "Сначала укажите путь к секрету" : undefined
          }
          onClick={load}
        >
          <KeyRound />
          {mapped ? "Изменить сопоставление" : "Сопоставить ключи"}
        </Button>

        {mapped ? (
          <span className="text-xs text-muted-foreground">
            логин <code className="font-mono">{value.usernameKey}</code>, пароль{" "}
            <code className="font-mono">{value.passwordKey}</code>
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">
            автоопределение по именам username / password
          </span>
        )}
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Ключи секрета</DialogTitle>
            <DialogDescription className="break-all">
              <code className="font-mono text-xs">{path}</code>
            </DialogDescription>
          </DialogHeader>

          {pending && (
            <p className="text-sm text-muted-foreground">Читаю секрет…</p>
          )}
          {error && <Alert variant="destructive">{error}</Alert>}

          {!pending && !error && keys.length === 0 && (
            <Alert variant="info">В секрете нет полей</Alert>
          )}

          {!pending && !error && keys.length > 0 && (
            <div className="flex flex-col gap-4">
              <Alert variant="info">
                Значения полей не читаются — Gemini показывает только их имена
              </Alert>
              <Field label="Логин берём из поля">
                <Select
                  value={draft.usernameKey}
                  onValueChange={(v) => setDraft({ ...draft, usernameKey: v })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="— выбрать —" />
                  </SelectTrigger>
                  <SelectContent>
                    {keys.map((k) => (
                      <SelectItem key={k} value={k} className="font-mono">
                        {k}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Пароль берём из поля">
                <Select
                  value={draft.passwordKey}
                  onValueChange={(v) => setDraft({ ...draft, passwordKey: v })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="— выбрать —" />
                  </SelectTrigger>
                  <SelectContent>
                    {keys.map((k) => (
                      <SelectItem key={k} value={k} className="font-mono">
                        {k}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                onChange({ usernameKey: "", passwordKey: "" });
                setOpen(false);
                toast.success("Сопоставление сброшено — будет автоопределение");
              }}
            >
              Сбросить
            </Button>
            <Button
              type="button"
              disabled={!draft.usernameKey || !draft.passwordKey}
              onClick={() => {
                onChange(draft);
                setOpen(false);
              }}
            >
              Применить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
