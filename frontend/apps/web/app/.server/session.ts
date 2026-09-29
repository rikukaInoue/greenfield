import { createFileSessionStorage } from "@react-router/node";
import { createCookieSessionStorage } from "react-router";
import { env } from "./env";

// SessionData はサーバー側ストアに置くセッションの中身。
// **Cookie に載るのはストアのセッションIDだけ**で、トークンはブラウザへ一切出ない（check #17）。
// 以前は署名 Cookie にトークンを直接入れており、httpOnly で JS からは読めないものの
// Cookie 値を base64 で復号すればトークンが見えた（stage-52 の気づき2）。
export type SessionData = {
  accessToken: string;
  refreshToken?: string;
  idToken?: string;
  // expiresAt は epoch 秒。0 は devtoken（期限管理しない）
  expiresAt: number;
  subject: string;
  aal: number;
};

// ファイルストアは単一プロセス前提の検証ビルド用。複数インスタンスに載せるときは
// Redis / DB へ差し替える（この module の export だけ見ればよい形にしてある）。
export const sessionStorage = createFileSessionStorage<SessionData, { flash: string }>({
  dir: env.sessionDir,
  cookie: {
    name: "__gf_session",
    httpOnly: true,
    sameSite: "lax",
    path: "/",
    secure: env.secureCookie,
    secrets: [env.sessionSecret],
    maxAge: 60 * 60 * 8,
  },
});

// oidcFlowStorage は認可リクエストの相関値（state / nonce / PKCE verifier）を
// callback まで持ち回るための短命 Cookie。ログイン前なのでセッションはまだ無い。
export const oidcFlowStorage = createCookieSessionStorage<{ flow: string }>({
  cookie: {
    name: "__gf_oidc",
    httpOnly: true,
    sameSite: "lax",
    path: "/",
    secure: env.secureCookie,
    secrets: [env.sessionSecret],
    maxAge: 60 * 10,
  },
});
