import { createFileSessionStorage } from "@react-router/node";
import { env } from "./env";

// SessionData はサーバー側ストアに置くセッションの中身。
// **Cookie に載るのはストアのセッションIDだけ**で、トークンはブラウザへ一切出ない（check #17）。
// admin は操作の危険度が高いぶん、この分離の価値が一番効く場所。
export type SessionData = {
  accessToken: string;
  subject: string;
  aal: number;
};

export const sessionStorage = createFileSessionStorage<SessionData, { flash: string }>({
  dir: env.sessionDir,
  cookie: {
    name: "__gf_admin_session",
    httpOnly: true,
    sameSite: "lax",
    path: "/",
    secure: env.secureCookie,
    secrets: [env.sessionSecret],
    maxAge: 60 * 60 * 2, // オペレータ操作は短命でよい
  },
});
