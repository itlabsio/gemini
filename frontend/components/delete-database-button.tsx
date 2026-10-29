"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
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

/**
 * Убирает из Gemini запись базы, которой больше нет на сервере. Удаляется и
 * журнал её запусков — поэтому только после подтверждения.
 */
export function DeleteDatabaseButton({
  dbName,
  headRole,
  disabled,
  onDelete,
  className,
}: {
  dbName: string;
  headRole: "source" | "target";
  disabled?: boolean;
  onDelete: () => void;
  className?: string;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        size="sm"
        variant="outline"
        onClick={() => setOpen(true)}
        disabled={disabled}
        className={className}
      >
        <Trash2 />
        Удалить
      </Button>

      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить «{dbName}» из Gemini?</AlertDialogTitle>
            <AlertDialogDescription>
              На сервере этой базы уже нет — удалится только её запись в Gemini
              вместе с журналом запусков, включая ссылки на дампы в S3. Сами
              дампы в бакете останутся.
              {headRole === "source" && (
                <>
                  {" "}
                  Если база с таким именем появится снова, Gemini заведёт её
                  как новую — сопоставление на target придётся настроить заново.
                </>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setOpen(false);
                onDelete();
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
