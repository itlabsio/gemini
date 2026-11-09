"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import { Pencil, PlugZap, Plus, Trash2 } from "lucide-react";
import type { NotificationChannel, NotificationProvider } from "@/lib/types";
import type { NotificationChannelPayload } from "@/lib/api";
import {
  deleteChannelAction,
  deleteSystemBotAction,
  saveChannelAction,
  setSystemBotAction,
  testChannelAction,
} from "@/app/projects/notification-actions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const PROVIDERS: { value: NotificationProvider; label: string }[] = [
  { value: "telegram", label: "Telegram" },
  { value: "max", label: "MAX" },
];

const TOKEN_PLACEHOLDER: Record<NotificationProvider, string> = {
  telegram: "123456789:AAH…",
  max: "токен с business.max.ru",
};

const providerLabel = (p: string) => PROVIDERS.find((x) => x.value === p)?.label ?? p;

function payloadOf(c: NotificationChannel): NotificationChannelPayload {
  return {
    name: c.name,
    provider: c.provider,
    chat_id: c.chat_id,
    thread_id: c.thread_id,
    bot_token: "",
    use_system_bot: !c.has_own_bot,
    on_failure: c.on_failure,
    on_success: c.on_success,
    enabled: c.enabled,
  };
}

/** Каналы проекта; project пустой — глобальные каналы (все события, только superuser). */
export function NotificationChannels({
  project,
  channels,
  systemBots,
}: {
  project: string;
  channels: NotificationChannel[];
  systemBots: Record<string, string>;
}) {
  const [pending, start] = useTransition();
  // editing: null — диалог закрыт; id "" — новый канал
  const [editing, setEditing] = useState<{
    id: string;
    hadOwnBot: boolean;
    form: NotificationChannelPayload;
  } | null>(null);
  const [toDelete, setToDelete] = useState<NotificationChannel | null>(null);
  const [testing, setTesting] = useState<string | null>(null);

  const form = editing?.form;
  const set = <K extends keyof NotificationChannelPayload>(k: K, v: NotificationChannelPayload[K]) =>
    setEditing((e) => (e ? { ...e, form: { ...e.form, [k]: v } } : e));

  function changeProvider(provider: NotificationProvider) {
    setEditing((e) =>
      e && {
        ...e,
        // токен и топик одного провайдера не подходят другому
        hadOwnBot: e.hadOwnBot && provider === e.form.provider,
        form: {
          ...e.form,
          provider,
          thread_id: provider === "telegram" ? e.form.thread_id : "",
          bot_token: "",
          use_system_bot: !!systemBots[provider],
        },
      },
    );
  }

  function add() {
    setEditing({
      id: "",
      hadOwnBot: false,
      form: {
        name: "",
        provider: "telegram",
        chat_id: "",
        thread_id: "",
        bot_token: "",
        use_system_bot: !!systemBots.telegram,
        on_failure: true,
        on_success: false,
        enabled: true,
      },
    });
  }

  function save() {
    if (!editing) return;
    const { id, form } = editing;
    start(async () => {
      const res = await saveChannelAction(project, id, form);
      if (res.ok) {
        toast.success(id ? "Канал сохранён" : "Канал добавлен");
        setEditing(null);
      } else toast.error(res.error);
    });
  }

  function remove() {
    if (!toDelete) return;
    const id = toDelete.id;
    start(async () => {
      const res = await deleteChannelAction(project, id);
      if (res.ok) toast.success("Канал удалён");
      else toast.error(res.error);
      setToDelete(null);
    });
  }

  function setEnabled(c: NotificationChannel, enabled: boolean) {
    start(async () => {
      const res = await saveChannelAction(project, c.id, { ...payloadOf(c), enabled });
      if (!res.ok) toast.error(res.error);
    });
  }

  async function test(c: NotificationChannel) {
    setTesting(c.id);
    const res = await testChannelAction(project, c.id);
    setTesting(null);
    if (res.ok) toast.success(`«${c.name}»: тестовое сообщение отправлено`);
    else toast.error(res.error);
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div className="flex flex-col gap-1.5">
            <CardTitle>{project ? "Каналы проекта" : "Глобальные каналы"}</CardTitle>
            <CardDescription>
              {project
                ? "Сюда приходят исходы dump и restore инстансов проекта. Настройки действуют только на этом инстансе Gemini: source сообщает о дампах, target — о восстановлениях"
                : "Получают исходы всех прогонов этого инстанса Gemini, в том числе инстансов без проекта"}
            </CardDescription>
          </div>
          <Button size="sm" onClick={add}>
            <Plus />
            Добавить
          </Button>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Имя</TableHead>
                <TableHead>Чат</TableHead>
                <TableHead>События</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {channels.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-medium">
                    {c.name}
                  </TableCell>
                  <TableCell>
                    <div className="font-mono text-xs">
                      {c.chat_id}
                      {c.thread_id ? ` / ${c.thread_id}` : ""}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {providerLabel(c.provider)} ·{" "}
                      {c.has_own_bot
                        ? `свой бот ${c.bot_name}`
                        : `системный бот ${systemBots[c.provider] ?? "(не настроен)"}`}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1.5">
                      {c.on_failure && <Badge variant="outline">провалы</Badge>}
                      {c.on_success && <Badge variant="outline">успехи</Badge>}
                    </div>
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Switch
                      className="mr-2 align-middle"
                      checked={c.enabled}
                      disabled={pending}
                      aria-label={c.enabled ? "Выключить канал" : "Включить канал"}
                      title={c.enabled ? "Канал включён" : "Канал выключен"}
                      onCheckedChange={(v) => setEnabled(c, v)}
                    />
                    <Button
                      variant="ghost-success"
                      size="icon"
                      aria-label="Отправить тестовое сообщение"
                      title="Отправить тестовое сообщение"
                      disabled={testing !== null}
                      onClick={() => test(c)}
                    >
                      <PlugZap className={testing === c.id ? "animate-pulse" : undefined} />
                    </Button>
                    <Button
                      variant="ghost-warning"
                      size="icon"
                      aria-label="Изменить"
                      title="Изменить канал"
                      onClick={() => setEditing({ id: c.id, hadOwnBot: c.has_own_bot, form: payloadOf(c) })}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost-destructive"
                      size="icon"
                      aria-label="Удалить"
                      title="Удалить канал"
                      onClick={() => setToDelete(c)}
                    >
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {channels.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="py-10 text-center text-muted-foreground">
                    Каналов нет
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={editing !== null} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing?.id ? "Изменить канал" : "Новый канал"}</DialogTitle>
            <DialogDescription>
              Добавьте бота в чат до проверки канала. Токен хранится в БД Gemini в зашифрованном виде и не показывается
            </DialogDescription>
          </DialogHeader>
          {form && (
            <div className="grid grid-cols-2 gap-4">
              <Field label="Мессенджер" className="col-span-2">
                <Select
                  value={form.provider}
                  onValueChange={(v) => changeProvider(v as NotificationProvider)}
                >
                  <SelectTrigger aria-label="Мессенджер">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PROVIDERS.map((p) => (
                      <SelectItem key={p.value} value={p.value}>
                        {p.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field label="Имя" htmlFor="nc_name" className="col-span-2">
                <Input id="nc_name" value={form.name} onChange={(e) => set("name", e.target.value)} />
              </Field>
              <Field
                label="ID чата или канала"
                htmlFor="nc_chat"
                className={form.provider === "telegram" ? undefined : "col-span-2"}
                hint={
                  form.provider === "max"
                    ? "У чатов и каналов MAX ID отрицательный, бот должен иметь право публикации сообщений"
                    : undefined
                }
              >
                <Input
                  id="nc_chat"
                  value={form.chat_id}
                  onChange={(e) => set("chat_id", e.target.value)}
                  placeholder={form.provider === "telegram" ? "-100123456789 или @channel" : "-123456789"}
                />
              </Field>
              {form.provider === "telegram" && (
                <Field label="ID топика" htmlFor="nc_thread" hint="Для форум-групп, необязательно">
                  <Input
                    id="nc_thread"
                    value={form.thread_id}
                    onChange={(e) => set("thread_id", e.target.value)}
                  />
                </Field>
              )}
              <Field
                label="Бот"
                className="col-span-2"
                hint={
                  form.use_system_bot && systemBots[form.provider]
                    ? `Добавьте бота ${systemBots[form.provider]} в чат с правом публикации сообщений`
                    : undefined
                }
              >
                <Select
                  value={form.use_system_bot ? "system" : "own"}
                  onValueChange={(v) => set("use_system_bot", v === "system")}
                >
                  <SelectTrigger aria-label="Бот">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="system" disabled={!systemBots[form.provider]}>
                      Системный: {systemBots[form.provider] ?? "не настроен"}
                    </SelectItem>
                    <SelectItem value="own">{editing?.hadOwnBot ? "Свой бот" : "Добавить нового"}</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              {!form.use_system_bot && (
                <Field label="Токен бота" htmlFor="nc_token" className="col-span-2">
                  <Input
                    id="nc_token"
                    type="password"
                    value={form.bot_token}
                    onChange={(e) => set("bot_token", e.target.value)}
                    placeholder={editing?.hadOwnBot ? "не менять" : TOKEN_PLACEHOLDER[form.provider]}
                    autoComplete="new-password"
                  />
                </Field>
              )}
              <Field label="События" className="col-span-2">
                <div className="flex flex-wrap gap-6">
                  {(
                    [
                      ["on_success", "Успешные запуски"],
                      ["on_failure", "Провалы"],
                    ] as const
                  ).map(([k, label]) => (
                    <div key={k} className="flex items-center gap-2">
                      <Checkbox id={`nc_${k}`} checked={form[k]} onCheckedChange={(v) => set(k, v === true)} />
                      <Label htmlFor={`nc_${k}`}>{label}</Label>
                    </div>
                  ))}
                </div>
              </Field>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditing(null)} disabled={pending}>
              Отмена
            </Button>
            <Button onClick={save} disabled={pending}>
              {pending ? "Сохранение…" : "Сохранить"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={toDelete !== null} onOpenChange={(o) => !o && setToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить канал «{toDelete?.name}»?</AlertDialogTitle>
            <AlertDialogDescription>Уведомления в этот чат приходить перестанут</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                remove();
              }}
              disabled={pending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {pending ? "Удаление…" : "Удалить"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** Токены системных ботов (superuser). Значения не отображаются. */
export function SystemBots({ systemBots }: { systemBots: Record<string, string> }) {
  const [pending, start] = useTransition();
  const [tokens, setTokens] = useState<Record<string, string>>({});
  const [toDelete, setToDelete] = useState<NotificationProvider | null>(null);

  function save(provider: NotificationProvider) {
    start(async () => {
      const res = await setSystemBotAction(provider, tokens[provider] ?? "");
      if (res.ok) {
        toast.success("Токен сохранён");
        setTokens((t) => ({ ...t, [provider]: "" }));
      } else toast.error(res.error);
    });
  }

  function remove() {
    if (!toDelete) return;
    const provider = toDelete;
    start(async () => {
      const res = await deleteSystemBotAction(provider);
      if (res.ok) toast.success("Системный бот удалён");
      else toast.error(res.error);
      setToDelete(null);
    });
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Системные боты</CardTitle>
          <CardDescription>
            Админы проектов могут выбрать системного бота для своих каналов, не видя его токен
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {PROVIDERS.map((p) => (
            <Field
              key={p.value}
              label={systemBots[p.value] ? `${p.label}: ${systemBots[p.value]}` : p.label}
              htmlFor={`bot_${p.value}`}
            >
              <div className="flex gap-2">
                <Input
                  id={`bot_${p.value}`}
                  type="password"
                  value={tokens[p.value] ?? ""}
                  onChange={(e) => setTokens((t) => ({ ...t, [p.value]: e.target.value }))}
                  placeholder={systemBots[p.value] ? "введите, чтобы заменить" : TOKEN_PLACEHOLDER[p.value]}
                  autoComplete="new-password"
                />
                <Button disabled={pending || !tokens[p.value]?.trim()} onClick={() => save(p.value)}>
                  Сохранить
                </Button>
                {systemBots[p.value] && (
                  <Button
                    variant="ghost-destructive"
                    size="icon"
                    aria-label={`Удалить системного бота ${p.label}`}
                    title="Удалить системного бота"
                    onClick={() => setToDelete(p.value)}
                  >
                    <Trash2 />
                  </Button>
                )}
              </div>
            </Field>
          ))}
        </CardContent>
      </Card>

      <AlertDialog open={toDelete !== null} onOpenChange={(o) => !o && setToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить системного бота?</AlertDialogTitle>
            <AlertDialogDescription>
              Все каналы, которые его используют, в том числе каналы проектов, перестанут получать
              уведомления
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                remove();
              }}
              disabled={pending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {pending ? "Удаление…" : "Удалить"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
