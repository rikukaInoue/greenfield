import { Outlet, redirect } from "react-router";
import type { Route } from "./+types/authed";
import { photoClientContext, requestIdContext, traceparentContext, viewerContext } from "../context";
import { createPhotoClient } from "../.server/photo";
import { sessionStorage } from "../.server/session";

const authMiddleware: Route.MiddlewareFunction = async ({ request, context }) => {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  const token = session.get("accessToken");
  if (!context.get(viewerContext) || !token) {
    const url = new URL(request.url);
    throw redirect(`/login?${new URLSearchParams({ returnTo: url.pathname + url.search })}`);
  }
  context.set(photoClientContext, createPhotoClient(token, context.get(requestIdContext), context.get(traceparentContext)));
};

export const middleware: Route.MiddlewareFunction[] = [authMiddleware];

export default function Authed() {
  return <Outlet />;
}
