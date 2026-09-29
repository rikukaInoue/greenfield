import { redirect } from "react-router";
import type { Route } from "./+types/logout";
import { env } from "../.server/env";
import { endSessionUrl } from "../.server/oidc";
import { sessionStorage } from "../.server/session";

export async function action({ request }: Route.ActionArgs) {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  const idToken = session.get("idToken");
  const headers = { "Set-Cookie": await sessionStorage.destroySession(session) };
  // OIDC ログインなら Keycloak 側のセッションも終わらせる（ローカル破棄だけだと
  // 次のログインで即座に同じユーザーに戻り、「ログアウトした」体験にならない）
  if (env.oidc && idToken) {
    const url = new URL(request.url);
    const end = await endSessionUrl(idToken, `${url.origin}/login`);
    if (end) return redirect(end, { headers });
  }
  return redirect("/login", { headers });
}

export function loader() {
  return redirect("/");
}
