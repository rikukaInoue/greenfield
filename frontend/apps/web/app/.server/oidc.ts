// oidc は Keycloak との Authorization Code フロー（confidential client）。
// SSR がトークンを預かり、ブラウザには一切出さない（check #17）。
//
// 依存ライブラリを使わないのは Go 側（core/authz/oidcauthn）と同じ判断:
// issuer 固定・code フロー固定・PKCE S256 固定に絞れば、必要なのは
// discovery の fetch とフォーム POST だけで、ライブラリの自由度（暗黙のフォールバック）ごと消せる。
import { createHash, randomBytes } from "node:crypto";
import { env } from "./env.ts";

export type OidcTokens = {
  accessToken: string;
  refreshToken?: string;
  idToken?: string;
  // expiresAt は epoch 秒。この時刻を過ぎたら refresh する
  expiresAt: number;
};

type Discovery = {
  authorization_endpoint: string;
  token_endpoint: string;
  end_session_endpoint?: string;
  issuer: string;
};

let discovered: Discovery | null = null;

// discovery は OP のエンドポイントを issuer から引く。起動後の初回だけ fetch し、以後は使い回す。
// issuer の食い違い（internal-07 の罠）は Go 側と同じく早期に落とす。
async function discovery(): Promise<Discovery> {
  if (discovered) return discovered;
  const issuer = env.oidc!.issuer;
  const res = await fetch(`${issuer.replace(/\/$/, "")}/.well-known/openid-configuration`);
  if (!res.ok) throw new Error(`OIDC discovery が失敗（${res.status}）: ${issuer}`);
  const d = (await res.json()) as Discovery;
  if (d.issuer !== issuer) {
    throw new Error(`issuer が食い違う: 設定=${issuer} discovery=${d.issuer}（internal-07 の罠）`);
  }
  discovered = d;
  return d;
}

const b64url = (b: Buffer) => b.toString("base64url");

// AuthRequest は認可リクエストの相関値。callback まで署名 Cookie で持ち回る。
export type AuthRequest = { state: string; nonce: string; verifier: string; returnTo: string };

export function newAuthRequest(returnTo: string): AuthRequest {
  return {
    state: b64url(randomBytes(24)),
    nonce: b64url(randomBytes(24)),
    verifier: b64url(randomBytes(48)),
    returnTo,
  };
}

// authorizationUrl は Keycloak のログイン画面へのリダイレクト先を組む。
export async function authorizationUrl(req: AuthRequest, redirectUri: string): Promise<string> {
  const d = await discovery();
  const url = new URL(d.authorization_endpoint);
  url.search = new URLSearchParams({
    client_id: env.oidc!.clientId,
    response_type: "code",
    scope: "openid",
    redirect_uri: redirectUri,
    state: req.state,
    nonce: req.nonce,
    code_challenge: b64url(createHash("sha256").update(req.verifier).digest()),
    code_challenge_method: "S256",
  }).toString();
  return url.toString();
}

type TokenResponse = {
  access_token: string;
  refresh_token?: string;
  id_token?: string;
  expires_in: number;
};

async function tokenRequest(form: Record<string, string>): Promise<OidcTokens> {
  const d = await discovery();
  const res = await fetch(d.token_endpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      client_id: env.oidc!.clientId,
      client_secret: env.oidc!.clientSecret,
      ...form,
    }),
  });
  const body = await res.text();
  if (!res.ok) throw new Error(`トークンエンドポイントが ${res.status}: ${body.slice(0, 200)}`);
  const t = JSON.parse(body) as TokenResponse;
  return {
    accessToken: t.access_token,
    refreshToken: t.refresh_token,
    idToken: t.id_token,
    // 期限の30秒前に取り直す余白（core/httpclient の expirySlack と同じ理由）
    expiresAt: Math.floor(Date.now() / 1000) + t.expires_in - 30,
  };
}

// exchangeCode は認可コードをトークンへ交換する（client_secret + PKCE verifier）。
export function exchangeCode(code: string, verifier: string, redirectUri: string): Promise<OidcTokens> {
  return tokenRequest({
    grant_type: "authorization_code",
    code,
    code_verifier: verifier,
    redirect_uri: redirectUri,
  });
}

// refresh はリフレッシュトークンでアクセストークンを取り直す。
export function refresh(refreshToken: string): Promise<OidcTokens> {
  return tokenRequest({ grant_type: "refresh_token", refresh_token: refreshToken });
}

// claims はトークンのペイロードを**検証せずに**読む。SSR は OP から直接受け取った
// トークンしか扱わないため、表示用の subject / aal 取り出しに署名検証は不要
// （検証の責務は API 側の oidcauthn にある。二重に持つと鍵配布の面倒だけ増える）。
export function claims(token: string): { sub?: string; acr?: string; preferred_username?: string; nonce?: string } {
  const payload = token.split(".")[1];
  if (!payload) return {};
  try {
    return JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
  } catch {
    return {};
  }
}

// endSessionUrl は Keycloak 側のセッションも終わらせる URL。無ければ null（ローカル破棄のみ）。
export async function endSessionUrl(idToken: string | undefined, postLogoutRedirect: string): Promise<string | null> {
  const d = await discovery();
  if (!d.end_session_endpoint) return null;
  const url = new URL(d.end_session_endpoint);
  const q = new URLSearchParams({ post_logout_redirect_uri: postLogoutRedirect, client_id: env.oidc!.clientId });
  if (idToken) q.set("id_token_hint", idToken);
  url.search = q.toString();
  return url.toString();
}
