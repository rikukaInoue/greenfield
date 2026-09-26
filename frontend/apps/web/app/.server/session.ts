import { createCookieSessionStorage } from "react-router";
import { env } from "./env";

// SessionData はセッション Cookie の中身。アクセストークンはここにだけ置き、ブラウザの JS へは渡さない。
export type SessionData = { accessToken: string; subject: string; aal: number };

export const sessionStorage = createCookieSessionStorage<SessionData, { flash: string }>({
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
