"use client";

import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";
import { toast } from "sonner";
import { createInstanceAction, updateInstanceAction } from "@/app/instances/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/ui/field";
import { Alert } from "@/components/ui/alert";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { PageHeader } from "@/components/ui/page-header";
import { VaultKeyMapper, type KeyMapping } from "@/components/vault-key-mapper";
import type { AuthType, HeadRole, Instance, RestoreMode } from "@/lib/types";

export function InstanceForm({
  initial,
  vaultEnabled,
  headRole,
  caDonors = [],
}: {
  initial?: Instance;
  vaultEnabled: boolean;
  headRole: HeadRole;
  /** Инстансы этой головы с кастомным CA — источники для копирования PEM. */
  caDonors?: { id: string; name: string; ssl_root_cert: string }[];
}) {
  const router = useRouter();
  const editing = !!initial;
  // Существующий vault-инстанс оставляем выбираемым, даже если Vault сейчас
  // выключен, — чтобы не терять настройку при сохранении.
  const vaultSelectable = vaultEnabled || initial?.auth_type === "vault";
  const [pending, start] = useTransition();
  const [error, setError] = useState<string | null>(null);

  const [sslMode, setSslMode] = useState(initial?.ssl_mode ?? "verify-full");
  const verifyMode = sslMode === "verify-ca" || sslMode === "verify-full";
  // Кастомный CA нужен только self-hosted инстансам со своим сертификатом; для
  // managed Yandex хватает встроенного trust store образа.
  const [caSource, setCaSource] = useState<"builtin" | "custom">(
    initial?.ssl_root_cert ? "custom" : "builtin",
  );
  const [rootCert, setRootCert] = useState(initial?.ssl_root_cert ?? "");
  const [excluded, setExcluded] = useState(
    (initial?.excluded_databases ?? []).join("\n"),
  );
  const [restoreMode, setRestoreMode] = useState<RestoreMode>(
    initial?.restore_mode ?? "recreate",
  );
  const [authType, setAuthType] = useState<AuthType>(
    initial?.auth_type ?? "plain",
  );
  // Путь контролируемый: модалка сопоставления ключей читает секрет именно по нему.
  const [vaultPath, setVaultPath] = useState(initial?.vault_path ?? "");
  const [vaultKeys, setVaultKeys] = useState<KeyMapping>({
    usernameKey: initial?.vault_username_key ?? "",
    passwordKey: initial?.vault_password_key ?? "",
  });

  function onSubmit(fd: FormData) {
    setError(null);
    const payload = {
      name: String(fd.get("name") ?? ""),
      host: String(fd.get("host") ?? ""),
      port: Number(fd.get("port") ?? 5432),
      ssl_mode: sslMode,
      ssl_root_cert:
        verifyMode && caSource === "custom"
          ? rootCert.trim() || undefined
          : undefined,
      discovery_db: String(fd.get("discovery_db") ?? "") || undefined,
      restore_mode: headRole === "target" ? restoreMode : undefined,
      excluded_databases: excluded
        .split(/[\s,]+/)
        .map((s) => s.trim())
        .filter(Boolean),
      auth_type: authType,
      plain_username: String(fd.get("plain_username") ?? "") || undefined,
      plain_password: String(fd.get("plain_password") ?? "") || undefined,
      vault_path: vaultPath || undefined,
      vault_role: String(fd.get("vault_role") ?? "") || undefined,
      vault_username_key: vaultKeys.usernameKey || undefined,
      vault_password_key: vaultKeys.passwordKey || undefined,
    };
    start(async () => {
      const res = editing
        ? await updateInstanceAction(initial.id, payload)
        : await createInstanceAction(payload);
      if (res.ok) {
        toast.success(editing ? "Инстанс обновлён" : "Инстанс создан");
        router.push(
          editing
            ? `/instances/${initial.id}`
            : `/instances/${"id" in res ? res.id : ""}`,
        );
      } else {
        setError(res.error);
        toast.error(res.error);
      }
    });
  }

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <PageHeader
        title={editing ? `Инстанс ${initial.name}` : "Новый инстанс"}
        description={
          editing
            ? "Пароль оставьте пустым, чтобы не менять"
            : "После создания на странице инстанса запустите discovery и отметьте базы"
        }
      />

      {error && <Alert variant="destructive">{error}</Alert>}

      <form action={onSubmit} className="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Подключение</CardTitle>
            <CardDescription>
              Куда Gemini подключается для discovery и снятия дампов
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <Field label="Имя" htmlFor="name">
              <Input
                id="name"
                name="name"
                required
                defaultValue={initial?.name}
                placeholder="prod-core"
              />
            </Field>

            <div className="flex gap-4">
              <Field label="Хост" htmlFor="host" className="flex-1">
                <Input
                  id="host"
                  name="host"
                  required
                  defaultValue={initial?.host}
                  placeholder="c-xxx.mdb.yandexcloud.net"
                />
              </Field>
              <Field label="Порт" htmlFor="port" className="w-28">
                <Input
                  id="port"
                  name="port"
                  type="number"
                  defaultValue={initial?.port ?? 6432}
                />
              </Field>
            </div>

            <Field label="SSL mode">
              <Select value={sslMode} onValueChange={setSslMode}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["disable", "require", "verify-ca", "verify-full"].map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {verifyMode && (
              <Field
                label="CA-сертификат"
                hint="Чем проверять сертификат сервера. Для Yandex Managed и публичных CA — встроенный. Для self-hosted со своим CA — кастомный."
              >
                <Select
                  value={caSource}
                  onValueChange={(v) => setCaSource(v as "builtin" | "custom")}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="builtin">
                      Встроенный (Yandex Cloud + публичные)
                    </SelectItem>
                    <SelectItem value="custom">Кастомный</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            )}

            {verifyMode && caSource === "custom" && (
              <Field
                label="PEM корневого CA"
                htmlFor="ssl_root_cert"
                hint="Вставьте PEM, загрузите файл .pem/.crt или скопируйте из другого инстанса. Несколько сертификатов в одном PEM — допустимо."
              >
                {caDonors.length > 0 && (
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-xs text-muted-foreground">
                      Скопировать из инстанса:
                    </span>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button type="button" variant="outline" size="sm">
                          Выбрать
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="start">
                        {caDonors.map((d) => (
                          <DropdownMenuItem
                            key={d.id}
                            onSelect={() => {
                              setRootCert(d.ssl_root_cert);
                              toast.success(`CA скопирован из «${d.name}»`);
                            }}
                          >
                            {d.name}
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                )}
                <Textarea
                  id="ssl_root_cert"
                  value={rootCert}
                  onChange={(e) => setRootCert(e.target.value)}
                  rows={6}
                  required
                  placeholder={"-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----"}
                  className="font-mono text-xs"
                />
                <input
                  type="file"
                  accept=".pem,.crt,.cer"
                  className="text-xs text-muted-foreground file:mr-3 file:rounded-md file:border file:border-input file:bg-transparent file:px-3 file:py-1 file:text-xs"
                  onChange={(e) => {
                    const f = e.target.files?.[0];
                    if (f) f.text().then(setRootCert);
                  }}
                />
              </Field>
            )}

            <Field
              label="База для discovery"
              htmlFor="discovery_db"
              hint="К какой базе подключаться для чтения списка баз. Пусто → postgres. Для Yandex Managed PostgreSQL укажите существующую пользовательскую базу (базы postgres в пуле нет)"
            >
              <Input
                id="discovery_db"
                name="discovery_db"
                defaultValue={initial?.discovery_db}
                placeholder="postgres"
              />
            </Field>

            {headRole === "target" && (
              <Field
                label="Стратегия restore"
                hint="recreate: RENAME старой базы → CREATE → накат → DROP (self-hosted, нужен суперюзер и права CREATE DATABASE). in_place: накат поверх существующей базы дампом с --clean --if-exists (Yandex MDB — базу и роль-владельца заводят через консоль)."
              >
                <Select
                  value={restoreMode}
                  onValueChange={(v) => setRestoreMode(v as RestoreMode)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="recreate">
                      recreate (self-hosted)
                    </SelectItem>
                    <SelectItem value="in_place">in_place (MDB)</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            )}

            <Field
              label="Исключённые базы"
              htmlFor="excluded_databases"
              hint="Их discovery пропускает. По одной в строке. База postgres пропускается всегда и без этого списка."
            >
              <Textarea
                id="excluded_databases"
                value={excluded}
                onChange={(e) => setExcluded(e.target.value)}
                rows={3}
                placeholder={"template_app\nsandbox"}
                className="font-mono text-xs"
              />
            </Field>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Секрет доступа</CardTitle>
            <CardDescription>
              Логин/пароль напрямую или путь в Vault
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <Field
              label="Тип секрета"
              hint={
                !vaultEnabled ? "Vault не включён для этого Gemini" : undefined
              }
            >
              <Select
                value={authType}
                onValueChange={(v) => setAuthType(v as AuthType)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="plain">plain (логин/пароль)</SelectItem>
                  <SelectItem value="vault" disabled={!vaultSelectable}>
                    vault (path + role)
                  </SelectItem>
                </SelectContent>
              </Select>
            </Field>

            {authType === "plain" ? (
              <div className="flex gap-4">
                <Field
                  label="Пользователь"
                  htmlFor="plain_username"
                  className="flex-1"
                >
                  <Input
                    id="plain_username"
                    name="plain_username"
                    defaultValue={initial?.plain_username}
                  />
                </Field>
                <Field label="Пароль" htmlFor="plain_password" className="flex-1">
                  <Input
                    id="plain_password"
                    name="plain_password"
                    type="password"
                    placeholder={editing ? "без изменений" : ""}
                  />
                </Field>
              </div>
            ) : (
              <>
                <div className="flex gap-4">
                <Field
                  label="Vault path"
                  htmlFor="vault_path"
                  className="flex-1"
                  hint="Полный путь Vault API, как в `vault read` — вместе с mount и сегментом data/ для KV v2"
                >
                  <Input
                    id="vault_path"
                    name="vault_path"
                    value={vaultPath}
                    onChange={(e) => setVaultPath(e.target.value)}
                    placeholder="kv/data/prod/db-backup/core"
                    className="font-mono text-xs"
                  />
                </Field>
                <Field label="Vault role" htmlFor="vault_role" className="flex-1">
                  <Input
                    id="vault_role"
                    name="vault_role"
                    defaultValue={initial?.vault_role}
                  />
                </Field>
                </div>
                <Field
                  label="Ключи секрета"
                  hint="Поля секрета называются по-разному — укажите, какое из них логин, а какое пароль"
                >
                  <VaultKeyMapper
                    path={vaultPath}
                    value={vaultKeys}
                    onChange={setVaultKeys}
                  />
                </Field>
              </>
            )}
          </CardContent>
        </Card>

        <div className="flex gap-3">
          <Button type="submit" disabled={pending}>
            {pending
              ? "Сохранение…"
              : editing
                ? "Сохранить"
                : "Создать инстанс"}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={() =>
              router.push(editing ? `/instances/${initial.id}` : "/instances")
            }
          >
            Отмена
          </Button>
        </div>
      </form>
    </div>
  );
}
