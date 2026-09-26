import {
  Form,
  isRouteErrorResponse,
  Link,
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
  useRouteLoaderData,
} from "react-router";

import type { Route } from "./+types/root";
import { requestIdContext, viewerContext } from "./context";
import { sessionStorage } from "./.server/session";
import "./app.css";

const sessionMiddleware: Route.MiddlewareFunction = async ({ request, context }, next) => {
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  const subject = session.get("subject");
  context.set(viewerContext, subject ? { subject, aal: session.get("aal") ?? 1 } : null);
  context.set(requestIdContext, request.headers.get("X-Request-Id") ?? crypto.randomUUID());
  return next();
};

export const middleware: Route.MiddlewareFunction[] = [sessionMiddleware];

export function loader({ context }: Route.LoaderArgs) {
  return { viewer: context.get(viewerContext) };
}

export function meta() {
  return [{ title: "greenfield photos" }];
}

export function Layout({ children }: { children: React.ReactNode }) {
  const root = useRouteLoaderData<typeof loader>("root");
  const viewer = root?.viewer;
  return (
    <html lang="ja">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
      </head>
      <body className="min-h-screen bg-stone-50 text-stone-900 dark:bg-stone-950 dark:text-stone-100">
        <header className="border-b border-stone-200 dark:border-stone-800">
          <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-3">
            <Link to="/" className="font-semibold tracking-tight">
              greenfield photos
            </Link>
            {viewer && (
              <div className="flex items-center gap-3 text-sm">
                <span>
                  {viewer.subject}
                  <span className="ml-1 rounded bg-stone-200 px-1.5 py-0.5 text-xs dark:bg-stone-800">
                    AAL{viewer.aal}
                  </span>
                </span>
                <Form method="post" action="/logout">
                  <button className="text-stone-500 hover:underline">ログアウト</button>
                </Form>
              </div>
            )}
          </div>
        </header>
        <main className="mx-auto max-w-5xl px-4 py-6">{children}</main>
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export default function App() {
  return <Outlet />;
}

export function ErrorBoundary({ error }: Route.ErrorBoundaryProps) {
  let message = "エラー";
  let details = "予期しないエラーが発生しました。";
  let stack: string | undefined;

  if (isRouteErrorResponse(error)) {
    message = String(error.status);
    details = error.data?.detail ?? (error.status === 404 ? "ページが見つかりません。" : error.statusText || details);
  } else if (import.meta.env.DEV && error instanceof Error) {
    details = error.message;
    stack = error.stack;
  }

  return (
    <div>
      <h1 className="text-2xl font-semibold">{message}</h1>
      <p className="mt-2">{details}</p>
      {stack && (
        <pre className="mt-4 overflow-x-auto p-4 text-xs">
          <code>{stack}</code>
        </pre>
      )}
    </div>
  );
}
