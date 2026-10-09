"use client";

import { useState, useTransition } from "react";
import { toast } from "sonner";
import { Pencil, PlugZap, Plus, Trash2 } from "lucide-react";
import type { S3Bucket } from "@/lib/types";
import type { S3BucketPayload } from "@/lib/api";
import {
  deleteS3BucketAction,
  saveS3BucketAction,
  testS3BucketAction,
} from "@/app/settings/actions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
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

const EMPTY: S3BucketPayload = {
  name: "",
  endpoint: "storage.yandexcloud.net",
  region: "ru-central1",
  bucket: "",
  access_key_id: "",
  secret_access_key: "",
  path_style: true,
  use_ssl: true,
  enabled: true,
};

function payloadOf(b: S3Bucket): S3BucketPayload {
  return {
    name: b.name,
    endpoint: b.endpoint,
    region: b.region,
    bucket: b.bucket,
    access_key_id: b.access_key_id,
    secret_access_key: "",
    path_style: b.path_style,
    use_ssl: b.use_ssl,
    enabled: b.enabled,
  };
}

export function S3BucketsManager({ buckets }: { buckets: S3Bucket[] }) {
  const [pending, start] = useTransition();
  // editing: null — диалог закрыт; id "" — новый бакет
  const [editing, setEditing] = useState<{ id: string; form: S3BucketPayload } | null>(null);
  const [toDelete, setToDelete] = useState<S3Bucket | null>(null);
  const [testing, setTesting] = useState<string | null>(null);

  const set = <K extends keyof S3BucketPayload>(k: K, v: S3BucketPayload[K]) =>
    setEditing((e) => (e ? { ...e, form: { ...e.form, [k]: v } } : e));

  function save() {
    if (!editing) return;
    const { id, form } = editing;
    start(async () => {
      const res = await saveS3BucketAction(id, form);
      if (res.ok) {
        toast.success(id ? "Бакет сохранён" : "Бакет добавлен");
        setEditing(null);
      } else toast.error(res.error);
    });
  }

  function remove() {
    if (!toDelete) return;
    const id = toDelete.id;
    start(async () => {
      const res = await deleteS3BucketAction(id);
      if (res.ok) toast.success("Бакет удалён");
      else toast.error(res.error);
      setToDelete(null);
    });
  }

  async function test(b: S3Bucket) {
    setTesting(b.id);
    const res = await testS3BucketAction(b.id);
    setTesting(null);
    if (res.ok) toast.success(`«${b.name}»: бакет доступен`);
    else toast.error(res.error);
  }

  const form = editing?.form;

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div className="flex flex-col gap-1.5">
            <CardTitle>S3-бакеты</CardTitle>
            <CardDescription>
              Source заливает каждый дамп во все включённые бакеты, Target
              восстанавливает из первого бакета, где дамп есть
            </CardDescription>
          </div>
          <Button size="sm" onClick={() => setEditing({ id: "", form: EMPTY })}>
            <Plus />
            Добавить
          </Button>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Имя</TableHead>
                <TableHead>Бакет</TableHead>
                <TableHead>Статус</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {buckets.map((b) => (
                <TableRow key={b.id}>
                  <TableCell className="font-medium">
                    <div className="flex items-center gap-2">
                      {b.name}
                      {b.built_in && <Badge variant="secondary">Helm</Badge>}
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {b.endpoint}/{b.bucket}
                  </TableCell>
                  <TableCell>
                    {b.enabled ? (
                      <Badge variant="outline">включён</Badge>
                    ) : (
                      <Badge variant="secondary">выключен</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Проверить доступ"
                      disabled={!b.enabled || testing !== null}
                      onClick={() => test(b)}
                    >
                      <PlugZap className={testing === b.id ? "animate-pulse" : undefined} />
                    </Button>
                    {!b.built_in && (
                      <>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label="Изменить"
                          onClick={() => setEditing({ id: b.id, form: payloadOf(b) })}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label="Удалить"
                          onClick={() => setToDelete(b)}
                        >
                          <Trash2 />
                        </Button>
                      </>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {buckets.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="py-10 text-center text-muted-foreground">
                    Бакетов нет — dump и restore не запустятся
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
            <DialogTitle>{editing?.id ? "Изменить бакет" : "Новый бакет"}</DialogTitle>
            <DialogDescription>
              Ключи хранятся в БД Gemini в зашифрованном виде и передаются
              dump/restore-подам через секрет
            </DialogDescription>
          </DialogHeader>
          {form && (
            <div className="grid grid-cols-2 gap-4">
              <Field label="Имя" htmlFor="s3_name" className="col-span-2">
                <Input
                  id="s3_name"
                  value={form.name}
                  onChange={(e) => set("name", e.target.value)}
                  placeholder="yc-reserve"
                />
              </Field>
              <Field label="Endpoint" htmlFor="s3_endpoint" hint="host[:port] без схемы">
                <Input
                  id="s3_endpoint"
                  value={form.endpoint}
                  onChange={(e) => set("endpoint", e.target.value)}
                />
              </Field>
              <Field label="Регион" htmlFor="s3_region">
                <Input
                  id="s3_region"
                  value={form.region}
                  onChange={(e) => set("region", e.target.value)}
                />
              </Field>
              <Field label="Бакет" htmlFor="s3_bucket" className="col-span-2">
                <Input
                  id="s3_bucket"
                  value={form.bucket}
                  onChange={(e) => set("bucket", e.target.value)}
                  placeholder="gemini-dumps"
                />
              </Field>
              <Field label="Access key ID" htmlFor="s3_ak">
                <Input
                  id="s3_ak"
                  value={form.access_key_id}
                  onChange={(e) => set("access_key_id", e.target.value)}
                  autoComplete="off"
                />
              </Field>
              <Field label="Secret access key" htmlFor="s3_sk">
                <Input
                  id="s3_sk"
                  type="password"
                  value={form.secret_access_key}
                  onChange={(e) => set("secret_access_key", e.target.value)}
                  placeholder={editing?.id ? "не менять" : undefined}
                  autoComplete="new-password"
                />
              </Field>
              <div className="col-span-2 flex flex-wrap gap-6">
                {(
                  [
                    ["use_ssl", "HTTPS"],
                    ["path_style", "Path-style"],
                    ["enabled", "Включён"],
                  ] as const
                ).map(([k, label]) => (
                  <div key={k} className="flex items-center gap-2">
                    <Switch
                      id={`s3_${k}`}
                      checked={form[k]}
                      onCheckedChange={(v) => set(k, v)}
                    />
                    <Label htmlFor={`s3_${k}`}>{label}</Label>
                  </div>
                ))}
              </div>
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
            <AlertDialogTitle>Удалить бакет?</AlertDialogTitle>
            <AlertDialogDescription>
              «{toDelete?.name}» перестанет использоваться для новых дампов и
              restore. Объекты в самом бакете не удаляются
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
