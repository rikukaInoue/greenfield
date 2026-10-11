import { data, Form, redirect } from "react-router";
import type { Route } from "./+types/login";
import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { mintDevToken } from "../.server/devtoken";
import { env } from "../.server/env";
import { safeReturnTo } from "../.server/returnto";
import { sessionStorage } from "../.server/session";

export function loader({ request }: Route.LoaderArgs) {
  if (!env.devLogin) throw data("dev login is disabled", { status: 404 });
  const url = new URL(request.url);
  return {
    returnTo: safeReturnTo(url.searchParams.get("returnTo"), url),
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
  return redirect(safeReturnTo(String(form.get("returnTo")), request.url), {
    headers: { "Set-Cookie": await sessionStorage.commitSession(session) },
  });
}

// ログインは refine の authProvider.login / 公式の sign-in-form を使わず、素の <Form> で
// サーバ側 action に POST する。refine のログイン抽象は「ブラウザから API へクレデンシャルを
// 送る SPA」が前提で、サーバ側セッションに devtoken を積む形には乗らないため（比較の材料）。
// select は shadcn の Select（Radix）でなく素の <select> にしている——JS 無しでも送信でき、
// 非露出テストと playwright の selectOption が素直に通る。
export default function Login({ loaderData, actionData }: Route.ComponentProps) {
  return (
    <div className="mx-auto mt-12 max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle>開発用ログイン（admin）</CardTitle>
          <CardDescription>devtoken を発行してサーバ側セッションに保存します（開発環境のみ）。</CardDescription>
        </CardHeader>
        <CardContent>
          {loaderData.stepUp && (
            <Alert className="mb-4">
              <AlertTitle>再認証が必要です</AlertTitle>
              <AlertDescription>この操作には AAL2（多要素相当）が必要です。</AlertDescription>
            </Alert>
          )}
          <Form method="post" className="space-y-4">
            <input type="hidden" name="returnTo" value={loaderData.returnTo} />
            <div className="space-y-1.5">
              <Label htmlFor="subject">ユーザー名</Label>
              <Input id="subject" name="subject" required autoFocus defaultValue="carol" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="aal">認証保証レベル</Label>
              <select
                id="aal"
                name="aal"
                defaultValue={loaderData.stepUp ? "2" : "1"}
                className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
              >
                <option value="1">AAL1（パスワード相当）</option>
                <option value="2">AAL2（多要素相当）</option>
              </select>
            </div>
            {actionData?.error && (
              <Alert variant="destructive">
                <AlertDescription>{actionData.error}</AlertDescription>
              </Alert>
            )}
            <Button type="submit" className="w-full">
              ログイン
            </Button>
          </Form>
        </CardContent>
      </Card>
    </div>
  );
}
