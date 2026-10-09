"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Instance } from "@/lib/types";
import { Badge } from "@/components/ui/badge";
import { SecretBadge } from "@/components/secret-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function InstancesTable({ instances }: { instances: Instance[] }) {
  const router = useRouter();

  return (
    <div className="rounded-xl border bg-card">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Инстанс</TableHead>
            <TableHead>Роль</TableHead>
            <TableHead>Хост</TableHead>
            <TableHead>Секрет</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {instances.map((i) => {
            const href = `/instances/${i.id}`;
            return (
              <TableRow
                key={i.id}
                onClick={() => router.push(href)}
                className="cursor-pointer"
              >
                <TableCell>
                  <Link
                    href={href}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {i.name}
                  </Link>
                </TableCell>
                <TableCell>
                  <Badge
                    variant={i.role === "target" ? "info" : "secondary"}
                    className="capitalize"
                  >
                    {i.role}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs whitespace-nowrap">
                  {i.host}:{i.port}
                </TableCell>
                <TableCell>
                  <SecretBadge authType={i.auth_type} />
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
