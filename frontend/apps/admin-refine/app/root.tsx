import { Refine } from "@refinedev/core";
import routerProvider from "@refinedev/react-router";
import { CameraIcon, ImageIcon, UsersIcon } from "lucide-react";
import { isRouteErrorResponse, Links, Meta, Outlet, Scripts, ScrollRestoration, useLocation, useRouteLoaderData } from "react-router";

import type { Route } from "./+types/root";
import { AdminHeader } from "~/components/admin-header";
import { Sidebar } from "~/components/refine-ui/layout/sidebar";
import { Toaster } from "~/components/refine-ui/notification/toaster";
import { useNotificationProvider } from "~/components/refine-ui/notification/use-notification-provider";
import { ThemeProvider } from "~/components/refine-ui/theme/theme-provider";
import { SidebarInset, SidebarProvider } from "~/components/ui/sidebar";
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

// メニューは resources から導出される(レジストリの Sidebar が useMenu を使う)。
// 項目を足す = ここに1エントリ足す + ルートを1本足す。
const resources = [
  { name: "photos", list: "/", meta: { label: "写真", icon: <ImageIcon /> } },
  { name: "accounts", list: "/accounts", meta: { label: "アカウント", icon: <UsersIcon /> } },
  // gear の admin API は未実装。項目が増えるときの形だけ見せる(ページは準備中)
  { name: "gear", list: "/gear", meta: { label: "機材", icon: <CameraIcon /> } },
];

export default function App() {
  const root = useRouteLoaderData<typeof loader>("root");
  const viewer = root?.viewer ?? null;
  const location = useLocation();
  const currentPath = () => location.pathname + location.search;
  const shell = Boolean(viewer); // ログイン前はサイドバー無し
  return (
    <ThemeProvider defaultTheme="light" storageKey="gf-admin-theme">
      <Refine
        dataProvider={dataProvider}
        authProvider={createAuthProvider(currentPath)}
        routerProvider={routerProvider}
        notificationProvider={useNotificationProvider}
        resources={resources}
        options={{
          syncWithLocation: true,
          warnWhenUnsavedChanges: false,
          disableTelemetry: true,
          title: { text: "greenfield admin", icon: <ImageIcon className="size-4" /> },
        }}
      >
        {shell ? (
          <SidebarProvider>
            <Sidebar />
            <SidebarInset>
              <AdminHeader viewer={viewer} />
              <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-6">
                <Outlet />
              </main>
            </SidebarInset>
          </SidebarProvider>
        ) : (
          <main className="mx-auto w-full max-w-5xl px-4 py-6">
            <Outlet />
          </main>
        )}
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
