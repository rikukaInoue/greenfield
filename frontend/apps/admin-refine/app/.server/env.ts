// env はサーバー側の設定値。本番で開発用の既定値のまま起動しないよう、ここで止める（web と同じ流儀）。
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
  // admin リスナーに届くのはこのサーバだけ。ブラウザには一切出さない
  photoAdminUrl: required("PHOTO_ADMIN_URL", "http://localhost:8082"),
  sessionSecret: required("SESSION_SECRET", "dev-session-secret"),
  secureCookie: process.env.SECURE_COOKIE === "true",
  sessionDir: process.env.SESSION_DIR ?? ".data/sessions",
  // dev ログイン（devtoken）は開発環境でのみ。OIDC は比較の範囲外（web と同じ配線が入る）
  devLogin: development && process.env.DEV_LOGIN !== "false",
};
