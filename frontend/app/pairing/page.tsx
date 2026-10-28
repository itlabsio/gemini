import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { getMe, listPairings } from "@/lib/api";
import type { HeadPairing } from "@/lib/types";
import { IssueCodePanel, InitPairingPanel } from "@/components/pairing-panels";
import { PairingsTable } from "@/components/pairings-table";
import { AutoRefresh } from "@/components/auto-refresh";
import { PageHeader } from "@/components/ui/page-header";
import { Alert } from "@/components/ui/alert";

export const metadata: Metadata = { title: "Pairing" };

export default async function PairingPage() {
  const me = await getMe();
  if (!me.superuser) redirect("/instances");
  let pairings: HeadPairing[] = [];
  let error: string | null = null;
  try {
    pairings = await listPairings();
  } catch (e) {
    error = e instanceof Error ? e.message : "Ошибка загрузки";
  }

  // Source после выдачи кода ждёт, пока Target завершит обмен: строка pending
  // сама станет active по webhook'у от пира — опрашиваем, чтобы не жать F5.
  const awaitingPeer = pairings.some((p) => p.status === "pending");

  return (
    <div className="flex flex-col gap-6">
      <AutoRefresh active={awaitingPeer} />
      <PageHeader
        title="Gemini Pairing"
        description="Ручное установление доверия между Source и Target. Секреты между контурами не передаются"
      />

      {me.head_public_key && (
        <Alert variant="info">
          <p className="font-medium">
            Публичный ключ этого инстанса Gemini ({me.head_role})
          </p>
          <p className="font-mono text-xs break-all">{me.head_public_key}</p>
          <p className="text-muted-foreground">
            {me.head_role === "source"
              ? "Этим ключом Target'ы проверяют подпись дампов. "
              : null}
            У каждого инстанса Gemini свой ключ, поэтому с ключом второго
            инстанса он не совпадает. При pairing инстансы обмениваются ключами
            автоматически. Чтобы убедиться, что канал не подменён, сравните эту
            строку с колонкой «Ключ пира» у этой связи на втором инстансе — они
            должны совпасть символ в символ.
          </p>
        </Alert>
      )}

      {me.head_role === "source" ? (
        <IssueCodePanel
          lastIssuedAt={pairings
            .filter((p) => p.status === "pending")
            .map((p) => p.created_at)
            .sort()
            .at(-1)}
        />
      ) : (
        <InitPairingPanel />
      )}

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Существующие связи</h2>
        {error && <Alert variant="destructive">{error}</Alert>}
        <PairingsTable pairings={pairings} canDelete />
      </section>
    </div>
  );
}
