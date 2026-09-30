import { data, redirect } from "react-router";
import type { Route } from "./+types/auth.login";
import { env } from "../.server/env";
import { authorizationUrl, newAuthRequest } from "../.server/oidc";
import { safeReturnTo } from "../.server/returnto";
import { oidcFlowStorage } from "../.server/session";

// loader は Keycloak のログイン画面へ送り出す（Authorization Code + PKCE）。
// state / nonce / verifier は短命の署名 Cookie で callback まで持ち回る。
export async function loader({ request }: Route.LoaderArgs) {
  if (!env.oidc) throw data("OIDC is not configured", { status: 404 });
  const url = new URL(request.url);
  const req = newAuthRequest(safeReturnTo(url.searchParams.get("returnTo"), url));
  const flow = await oidcFlowStorage.getSession();
  flow.set("flow", JSON.stringify(req));
  const redirectUri = `${url.origin}/auth/callback`;
  return redirect(await authorizationUrl(req, redirectUri), {
    headers: { "Set-Cookie": await oidcFlowStorage.commitSession(flow) },
  });
}
