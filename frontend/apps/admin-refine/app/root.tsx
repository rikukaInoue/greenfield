import { Refine } from "@refinedev/core";
import routerProvider from "@refinedev/react-router";
import { Form, isRouteErrorResponse, Link, Links, Meta, Outlet, Scripts, ScrollRestoration, useLocation, useRouteLoaderData } from "react-router";

import type { Route } from "./+types/root";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Toaster } from "~/components/refine-ui/notification/toaster";
import { useNotificationProvider } from "~/components/refine-ui/notification/use-notification-provider";
import { ThemeProvider } from "~/components/refine-ui/theme/theme-provider";
import { requestIdContext, viewerContext } from "./context";
import { createAuthProvider, dataProvider } from "./providers";
import { securityHeaders } from "./.server/security";
import { sessionStorage } from "./.server/session";
import "./app.css";

const sessionMiddleware: Route.MiddlewareFunction = async ({ request, context }, next) => {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  const subject = session.get("subject");
  context.set(viewerContext, subject ? { subject, aal: session.get("aal") ?? 1 } : null);
  context.set(requestIdContext, request.headers.get("X-Request-Id") ?? crypto.randomUUID());
  return next();
};

const securityHeadersMiddleware: Route.MiddlewareFunction = async (_, next) => {
  const res = await next();
  for (const [name, value] of Object.entries(securityHeaders)) res.headers.set(name, value);
  return res;
};

export const middleware: Route.MiddlewareFunction[] = [securityHeadersMiddleware, sessionMiddleware];

export function loader({ context }: Route.LoaderArgs) {
  return { viewer: context.get(viewerContext) };
}

export function meta() {
  return [{ title: "greenfield admin (refine)" }];
}

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
      </head>
      <body className="min-h-screen">
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export default function App() {
  const root = useRouteLoaderData<typeof loader>("root");
  const viewer = root?.viewer;
  const location = useLocation();
  const currentPath = () => location.pathname + location.search;
  return (
    <ThemeProvider defaultTheme="light" storageKey="gf-admin-theme">
      <Refine
        dataProvider={dataProvider}
        authProvider={createAuthProvider(currentPath)}
        routerProvider={routerProvider}
        notificationProvider={useNotificationProvider}
        resources={[{ name: "photos", list: "/", meta: { label: "全ユーザーの写真" } }]}
        options={{ syncWithLocation: true, warnWhenUnsavedChanges: false, disableTelemetry: true }}
      >
        <header className="border-b">
          <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-3">
            <Link to="/" className="font-semibold tracking-tight">
              greenfield admin
            </Link>
            {viewer && (
              <div className="flex items-center gap-3 text-sm">
                <span>{viewer.subject}</span>
                <Badge variant={viewer.aal >= 2 ? "default" : "secondary"}>AAL{viewer.aal}</Badge>
                <Form method="post" action="/logout">
                  <Button type="submit" variant="ghost" size="sm">
                    ログアウト
                  </Button>
                </Form>
              </div>
            )}
          </div>
        </header>
        <main className="mx-auto max-w-5xl px-4 py-6">
          <Outlet />
        </main>
        <Toaster />
      </Refine>
    </ThemeProvider>
  );
}

export function ErrorBoundary({ error }: Route.ErrorBoundaryProps) {
  let message = "エラー";
  let details = "予期しないエラーが発生しました。";
  if (isRouteErrorResponse(error)) {
    message = String(error.status);
    details = error.data?.detail ?? (error.status === 404 ? "ページが見つかりません。" : error.statusText || details);
  } else if (import.meta.env.DEV && error instanceof Error) {
    details = error.message;
  }
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold">{message}</h1>
      <p className="mt-2">{details}</p>
    </div>
  );
}
