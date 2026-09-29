import { Outlet, redirect } from "react-router";
import type { Route } from "./+types/authed";
import { gearClientContext, photoClientContext, requestIdContext, traceparentContext, viewerContext } from "../context";
import { createGearClient } from "../.server/gear";
import { refresh } from "../.server/oidc";
import { createPhotoClient } from "../.server/photo";
import { sessionStorage } from "../.server/session";

const authMiddleware: Route.MiddlewareFunction = async ({ request, context }) => {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  let token = session.get("accessToken");
  const toLogin = () => {
    const url = new URL(request.url);
    return redirect(`/login?${new URLSearchParams({ returnTo: url.pathname + url.search })}`);
  };
  if (!context.get(viewerContext) || !token) throw toLogin();

  // 期限切れは refresh で取り直す（expiresAt=0 は devtoken。期限管理しない）。
  // refresh も駄目ならログインへ——古いトークンで API を叩いて 401 の山を作らない
  const expiresAt = session.get("expiresAt") ?? 0;
  const refreshToken = session.get("refreshToken");
  if (expiresAt > 0 && Date.now() / 1000 >= expiresAt) {
    if (!refreshToken) throw toLogin();
    try {
      const t = await refresh(refreshToken);
      session.set("accessToken", t.accessToken);
      if (t.refreshToken) session.set("refreshToken", t.refreshToken);
      session.set("expiresAt", t.expiresAt);
      token = t.accessToken;
      // ストア側セッションの更新。Cookie の中身（セッションID）は変わらない
      await sessionStorage.commitSession(session);
    } catch {
      throw toLogin();
    }
  }

  const requestId = context.get(requestIdContext);
  const traceparent = context.get(traceparentContext);
  context.set(photoClientContext, createPhotoClient(token, requestId, traceparent));
  context.set(gearClientContext, createGearClient(token, requestId, traceparent));
};

export const middleware: Route.MiddlewareFunction[] = [authMiddleware];

export default function Authed() {
  return <Outlet />;
}
