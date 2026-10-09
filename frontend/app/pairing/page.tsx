import { getMe, listPairings } from "@/lib/api";
import type { HeadPairing } from "@/lib/types";
import { IssueCodePanel, InitPairingPanel } from "@/components/pairing-panels";
import { PairingsTable } from "@/components/pairings-table";
import { AutoRefresh } from "@/components/auto-refresh";
import { PageHeader } from "@/components/ui/page-header";
import { Alert } from "@/components/ui/alert";

export default async function PairingPage() {
  const me = await getMe();
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
            Публичный ключ этой головы ({me.head_role})
          </p>
          <p className="font-mono text-xs break-all">{me.head_public_key}</p>
          <p className="text-muted-foreground">
            Дампы подписываются приватной частью. При pairing головы
            обмениваются публичными ключами автоматически — сверьте эту строку с
            той, что показывает вторая голова, если хотите убедиться в
            подлинности канала.
          </p>
        </Alert>
      )}

      {me.role === "admin" ? (
        me.head_role === "source" ? (
          <IssueCodePanel
            lastIssuedAt={pairings
              .filter((p) => p.status === "pending")
              .map((p) => p.created_at)
              .sort()
              .at(-1)}
          />
        ) : (
          <InitPairingPanel />
        )
      ) : (
        <Alert variant="info">Управление pairing доступно роли admin</Alert>
      )}

      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold">Существующие связи</h2>
        {error && <Alert variant="destructive">{error}</Alert>}
        <PairingsTable pairings={pairings} canDelete={me.role === "admin"} />
      </section>
    </div>
  );
}
