"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { toast } from "sonner";
import { Plus, Trash2, Unlink } from "lucide-react";
import type { Project } from "@/lib/types";
import {
  createProjectAction,
  deleteProjectAction,
  unmanageProjectAction,
} from "@/app/projects/actions";
import { ManagedBadge } from "@/components/managed-badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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

export function ProjectsManager({ projects }: { projects: Project[] }) {
  const [pending, start] = useTransition();
  const [name, setName] = useState("");
  const [toDelete, setToDelete] = useState<Project | null>(null);

  function add() {
    const n = name.trim();
    if (!n) return;
    start(async () => {
      const res = await createProjectAction(n);
      if (res.ok) {
        toast.success(`Проект «${n}» создан`);
        setName("");
      } else toast.error(res.error);
    });
  }

  function unmanage(n: string) {
    start(async () => {
      const res = await unmanageProjectAction(n);
      if (res.ok) toast.success(`С проекта «${n}» снята метка Terraform`);
      else toast.error(res.error);
    });
  }

  function remove() {
    if (!toDelete) return;
    const n = toDelete.name;
    start(async () => {
      const res = await deleteProjectAction(n);
      if (res.ok) toast.success(`Проект «${n}» удалён`);
      else toast.error(res.error);
      setToDelete(null);
    });
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Список проектов</CardTitle>
          <CardDescription>
            Имя должно совпадать с названием группы Keycloak. Проект с инстансами удалить нельзя
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              add();
            }}
          >
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="название проекта"
              aria-label="Название проекта"
            />
            <Button type="submit" disabled={pending || !name.trim()}>
              <Plus />
              Создать
            </Button>
          </form>

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Проект</TableHead>
                <TableHead>Инстансов</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {projects.map((p) => (
                <TableRow key={p.name}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{p.name}</span>
                      <ManagedBadge managedBy={p.managed_by} />
                    </div>
                  </TableCell>
                  <TableCell>
                    {p.instance_count > 0 ? (
                      <Link
                        href={`/instances?project=${encodeURIComponent(p.name)}`}
                        title="Показать инстансы проекта"
                      >
                        <Badge variant="secondary" className="hover:bg-secondary/70">
                          {p.instance_count}
                        </Badge>
                      </Link>
                    ) : (
                      <Badge variant="secondary">0</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    {p.managed_by === "terraform" && (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="Снять метку Terraform"
                        title="Снять метку Terraform"
                        disabled={pending}
                        onClick={() => unmanage(p.name)}
                      >
                        <Unlink />
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Удалить"
                      disabled={p.instance_count > 0}
                      onClick={() => setToDelete(p)}
                    >
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {projects.length === 0 && (
                <TableRow>
                  <TableCell colSpan={3} className="py-10 text-center text-muted-foreground">
                    Проектов пока нет
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <AlertDialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить проект «{toDelete?.name}»?</AlertDialogTitle>
            <AlertDialogDescription>
              Участники группы потеряют доступ к проекту в Gemini. Сама группа в Keycloak не меняется.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction onClick={remove} disabled={pending}>
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
