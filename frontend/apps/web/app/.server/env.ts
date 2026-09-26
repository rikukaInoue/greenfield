// env はサーバー側の設定値。本番で開発用の既定値のまま起動しないよう、ここで止める。
const production = process.env.NODE_ENV === "production" && ["production", "prod"].includes(process.env.ENV ?? "");

function required(name: string, devDefault: string): string {
  const v = process.env[name];
  if (v) return v;
  if (production) throw new Error(`${name} が未設定`);
  return devDefault;
}

export const env = {
  photoApiUrl: required("PHOTO_API_URL", "http://localhost:8080"),
  sessionSecret: required("SESSION_SECRET", "dev-session-secret"),
  secureCookie: process.env.SECURE_COOKIE === "true",
  // devLogin は devtoken による擬似ログインを許可するか。Keycloak 配線後は false にする。
  devLogin: !production && process.env.DEV_LOGIN !== "false",
};
