import { data, Form, redirect } from "react-router";
import type { Route } from "./+types/login";
import { mintDevToken } from "../.server/devtoken";
import { env } from "../.server/env";
import { sessionStorage } from "../.server/session";

// safeReturnTo はオープンリダイレクトを防ぐため、同一オリジンのパスだけを通す。
function safeReturnTo(v: string | null): string {
  return v && v.startsWith("/") && !v.startsWith("//") ? v : "/";
}

export function loader({ request }: Route.LoaderArgs) {
  if (!env.devLogin) throw data("dev login is disabled", { status: 404 });
  const url = new URL(request.url);
  return {
    returnTo: safeReturnTo(url.searchParams.get("returnTo")),
    stepUp: url.searchParams.get("aal") === "2",
  };
}

export async function action({ request }: Route.ActionArgs) {
  if (!env.devLogin) throw data("dev login is disabled", { status: 404 });
  const form = await request.formData();
  const subject = String(form.get("subject") ?? "").trim();
  const aal = form.get("aal") === "2" ? 2 : 1;
  if (!/^[\w.@-]{1,64}$/.test(subject)) {
    return data({ error: "ユーザー名は英数字と ._@- の 1〜64 文字" }, { status: 400 });
  }
  const session = await sessionStorage.getSession(request.headers.get("Cookie"));
  session.set("accessToken", mintDevToken({ sub: subject, aal }));
  session.set("subject", subject);
  session.set("aal", aal);
  return redirect(safeReturnTo(String(form.get("returnTo"))), {
    headers: { "Set-Cookie": await sessionStorage.commitSession(session) },
  });
}

export default function Login({ loaderData, actionData }: Route.ComponentProps) {
  return (
    <div className="mx-auto max-w-sm">
      <h1 className="text-xl font-semibold">開発用ログイン</h1>
      <p className="mt-1 text-sm text-stone-500">
        devtoken を発行してセッションに保存します。Keycloak 配線までの暫定です。
      </p>
      {loaderData.stepUp && (
        <p className="mt-4 rounded border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-950">
          この操作には再認証（AAL2）が必要です。
        </p>
      )}
      <Form method="post" className="mt-6 space-y-4">
        <input type="hidden" name="returnTo" value={loaderData.returnTo} />
        <label className="block">
          <span className="text-sm">ユーザー名</span>
          <input
            name="subject"
            required
            autoFocus
            defaultValue="alice"
            className="mt-1 w-full rounded border border-stone-300 bg-white px-3 py-2 dark:border-stone-700 dark:bg-stone-900"
          />
        </label>
        <label className="block">
          <span className="text-sm">認証保証レベル</span>
          <select
            name="aal"
            defaultValue={loaderData.stepUp ? "2" : "1"}
            className="mt-1 w-full rounded border border-stone-300 bg-white px-3 py-2 dark:border-stone-700 dark:bg-stone-900"
          >
            <option value="1">AAL1（パスワード相当）</option>
            <option value="2">AAL2（多要素相当）</option>
          </select>
        </label>
        {actionData?.error && <p className="text-sm text-red-600">{actionData.error}</p>}
        <button className="w-full rounded bg-stone-900 px-3 py-2 text-white dark:bg-stone-100 dark:text-stone-900">
          ログイン
        </button>
      </Form>
    </div>
  );
}
