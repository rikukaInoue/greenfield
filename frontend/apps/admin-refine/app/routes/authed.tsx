import { Outlet, redirect } from "react-router";
import type { Route } from "./+types/authed";
import { adminClientContext, requestIdContext, viewerContext } from "../context";
import { createAdminClient } from "../.server/admin";
import { sessionStorage } from "../.server/session";

// authed 配下は全てログイン必須。トークンはここで取り出して admin API クライアントに渡す。
const authMiddleware: Route.MiddlewareFunction = async ({ request, context }) => {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  const token = session.get("accessToken");
  if (!context.get(viewerContext) || !token) {
    const url = new URL(request.url);
    // resource route への fetch が未ログインなら JSON 401（リダイレクトを fetch に食わせない）
    if (url.pathname.startsWith("/resources/")) {
      throw Response.json({ code: "unauthenticated", detail: "ログインが必要です" }, { status: 401 });
    }
    throw redirect(`/login?${new URLSearchParams({ returnTo: url.pathname + url.search })}`);
  }
  context.set(adminClientContext, createAdminClient(token, context.get(requestIdContext)));
};

export const middleware: Route.MiddlewareFunction[] = [authMiddleware];

export default function Authed() {
  return <Outlet />;
}
