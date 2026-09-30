import { data, redirect } from "react-router";
import type { Route } from "./+types/auth.callback";
import { env } from "../.server/env";
import { claims, exchangeCode, type AuthRequest } from "../.server/oidc";
import { safeReturnTo } from "../.server/returnto";
import { oidcFlowStorage, sessionStorage } from "../.server/session";

// loader は Keycloak からの戻り。code をトークンへ交換し、サーバー側セッションに保存する。
// **トークンは Set-Cookie に載らない**（Cookie はストアのセッションIDだけ。check #17）。
export async function loader({ request }: Route.LoaderArgs) {
  if (!env.oidc) throw data("OIDC is not configured", { status: 404 });
  const url = new URL(request.url);

  const flow = await oidcFlowStorage.getSession(request.headers.get("Cookie"));
  const raw = flow.get("flow");
  if (!raw) throw data("ログインをやり直してください（相関 Cookie がありません）", { status: 400 });
  const req = JSON.parse(raw) as AuthRequest;

  // state: 認可リクエストとの相関（CSRF・別フローの混線を止める）
  if (url.searchParams.get("state") !== req.state) {
    throw data("state が一致しません", { status: 400 });
  }
  const kcError = url.searchParams.get("error");
  if (kcError) {
    throw data(`ログインが完了しませんでした: ${kcError}`, { status: 400 });
  }
  const code = url.searchParams.get("code");
  if (!code) throw data("code がありません", { status: 400 });

  const tokens = await exchangeCode(code, req.verifier, `${url.origin}/auth/callback`);

  // nonce: 発行させた ID トークンが**このフローのため**のものかを確かめる（リプレイ対策）。
  // claims() は署名検証をしない読み取りだが、このトークンは OP から TLS/直結で
  // 受け取った直後のものなので、ここでの用途（相関確認・表示名）には足りる
  const id = claims(tokens.idToken ?? "");
  if (req.nonce && id.nonce !== req.nonce) {
    throw data("nonce が一致しません", { status: 400 });
  }
  const at = claims(tokens.accessToken);
  const subject = at.preferred_username ?? at.sub ?? "unknown";
  const aal = Number(at.acr) >= 2 ? 2 : 1;

  const session = await sessionStorage.getSession();
  session.set("accessToken", tokens.accessToken);
  if (tokens.refreshToken) session.set("refreshToken", tokens.refreshToken);
  if (tokens.idToken) session.set("idToken", tokens.idToken);
  session.set("expiresAt", tokens.expiresAt);
  session.set("subject", subject);
  session.set("aal", aal);

  const headers = new Headers();
  headers.append("Set-Cookie", await sessionStorage.commitSession(session));
  headers.append("Set-Cookie", await oidcFlowStorage.destroySession(flow));
  return redirect(safeReturnTo(req.returnTo, url), { headers });
}
