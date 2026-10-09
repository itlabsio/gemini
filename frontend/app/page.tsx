import { auth, signIn } from "@/lib/auth";
import { redirect } from "next/navigation";
import { ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default async function Home() {
  const session = await auth();
  // Только валидная сессия уводит на /instances. Иначе — редирект-цикл с proxy:
  // proxy гонит /instances → / при session.error, а `if (session)` гнал бы
  // обратно (объект сессии остаётся truthy даже с ошибкой refresh).
  // SessionGuard в layout до-чистит протухшую cookie через signOut.
  if (session?.accessToken && session.error !== "RefreshAccessTokenError") {
    redirect("/instances");
  }

  return (
    <div className="flex min-h-[70vh] items-center justify-center">
      <Card className="w-full max-w-md">
        <CardHeader className="items-center text-center">
          <span className="mb-2 grid size-12 place-items-center rounded-xl bg-primary text-primary-foreground text-lg font-bold">
            G
          </span>
          <CardTitle className="text-xl">Gemini · СРК</CardTitle>
          <CardDescription className="leading-relaxed">
            Система логического резервного копирования
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            action={async () => {
              "use server";
              await signIn("keycloak", { redirectTo: "/instances" });
            }}
          >
            <Button type="submit" size="lg" className="w-full">
              <ShieldCheck />
              Войти через Keycloak
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
