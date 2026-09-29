// env はサーバー側の設定値。本番で開発用の既定値のまま起動しないよう、ここで止める。
// 判定は runtimeenv の許可リストで行う（Go 側の core/runtimeenv と同じ意味論）。
// .ts を明記するのは Node の ESM 解決に合わせるため（node --test から直接読めるようにする）。
import { describe, isDevelopment } from "./runtimeenv.ts";

const development = isDevelopment();

function required(name: string, devDefault: string): string {
  const v = process.env[name];
  if (v) return v;
  if (!development) {
    throw new Error(`${name} が未設定（ENV=${describe()} では開発用の既定値を使えない）`);
  }
  return devDefault;
}

// OIDC（confidential client）は OIDC_ISSUER が設定されたときだけ有効になる。
// secret は issuer があるのに無ければ起動で落とす（本番で「なんとなく dev ログイン」に
// 落ちないため。fail-closed）。
function oidcFromEnv() {
  const issuer = process.env.OIDC_ISSUER;
  if (!issuer) return null;
  const clientSecret = process.env.SSR_CLIENT_SECRET ?? (development ? "ssr-dev-secret" : "");
  if (!clientSecret) {
    throw new Error(`SSR_CLIENT_SECRET が未設定（OIDC_ISSUER があるのに confidential client の秘密が無い）`);
  }
  return {
    issuer,
    clientId: process.env.SSR_CLIENT_ID ?? "ssr",
    clientSecret,
  };
}

export const env = {
  photoApiUrl: required("PHOTO_API_URL", "http://localhost:8080"),
  gearApiUrl: required("GEAR_API_URL", "http://localhost:8090"),
  sessionSecret: required("SESSION_SECRET", "dev-session-secret"),
  secureCookie: process.env.SECURE_COOKIE === "true",
  // sessionDir はサーバー側セッションストアの置き場。トークンは Cookie ではなくここに置く（#17）。
  sessionDir: process.env.SESSION_DIR ?? ".data/sessions",
  // devLogin は devtoken による擬似ログインを許可するか。OIDC 配線後も dev 環境の
  // fallback として残す（allinone の staticauthn 相手には devtoken しか通らない）。
  devLogin: development && process.env.DEV_LOGIN !== "false",
  oidc: oidcFromEnv(),
};
