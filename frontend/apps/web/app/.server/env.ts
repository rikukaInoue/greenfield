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

export const env = {
  photoApiUrl: required("PHOTO_API_URL", "http://localhost:8080"),
  sessionSecret: required("SESSION_SECRET", "dev-session-secret"),
  secureCookie: process.env.SECURE_COOKIE === "true",
  // devLogin は devtoken による擬似ログインを許可するか。Keycloak 配線後は false にする。
  devLogin: development && process.env.DEV_LOGIN !== "false",
};
