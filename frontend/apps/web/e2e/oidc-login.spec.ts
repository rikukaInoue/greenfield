import { expect, test } from "@playwright/test";

// #17 の OIDC 版: confidential client（Authorization Code + PKCE）でログインしても
// トークンはブラウザに出ない。dev ログイン（devtoken）とは別の SSR インスタンス
// （OIDC_WEB_URL。photo も OIDC モード）に対して流す。
//
// OIDC_WEB_URL が無ければ**skip として明示的に表示される**（silent pass にはならない）。
// CI の frontend ジョブは必ず設定するので、CI でこの検査が飛ぶことはない。
const webUrl = process.env.OIDC_WEB_URL;

const tokenLike = /dev\.[A-Za-z0-9+/=_-]{16,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/;

function cookiePayload(cookieValue: string): string {
  const signed = decodeURIComponent(cookieValue);
  const payload = signed.slice(0, signed.lastIndexOf("."));
  return Buffer.from(payload, "base64").toString("utf8");
}

test("Keycloak ログイン（confidential client）でもトークンはブラウザに出ない", async ({ browser }) => {
  test.skip(!webUrl, "OIDC_WEB_URL が未設定（OIDC スタックが無いローカルでは飛ばす。CI は必ず設定する）");
  const context = await browser.newContext({ baseURL: webUrl });
  const page = await context.newPage();
  const outgoing: { url: string; authorization?: string }[] = [];
  page.on("request", (r) => outgoing.push({ url: r.url(), authorization: r.headers()["authorization"] }));

  // 未ログインで / → /login → Keycloak のログイン画面へ
  await page.goto("/");
  await page.waitForURL(/\/realms\/greenfield\//);
  await page.locator("#username").fill("alice");
  await page.locator("#password").fill("alice-dev-password");
  await page.locator("button[type=submit]").first().click();

  // callback を経て戻り、ヘッダにユーザー名が出る
  await page.waitForURL((u) => u.origin === new URL(webUrl!).origin);
  await expect(page.getByRole("banner")).toContainText("alice");

  // Cookie はセッションIDのみ（復号してもトークンが無い）
  const session = (await context.cookies()).find((c) => c.name === "__gf_session");
  expect(session).toBeDefined();
  expect(session!.httpOnly).toBe(true);
  const payload = cookiePayload(session!.value);
  expect(payload).not.toContain("accessToken");
  expect(payload).not.toMatch(tokenLike);

  // JS から見える場所・HTML にトークンが無い
  const exposed = await page.evaluate(() => ({
    cookie: document.cookie,
    local: JSON.stringify({ ...localStorage }),
    session: JSON.stringify({ ...sessionStorage }),
    html: document.documentElement.outerHTML,
  }));
  expect(exposed.cookie).not.toContain("__gf_session");
  for (const v of [exposed.local, exposed.session, exposed.html]) expect(v).not.toMatch(tokenLike);

  // ブラウザから API へ直接は何も出ていない（Authorization 付きの通信ゼロ）
  expect(outgoing.filter((r) => r.authorization)).toEqual([]);
  expect(outgoing.filter((r) => /:(18080|18081|18082)\//.test(r.url))).toEqual([]);

  // ログアウト: ローカルセッション破棄 + Keycloak の end_session を経て、
  // 次のアクセスで**資格情報の入力フォームが再表示される**ことを確かめる。
  // KC 側のセッションが残っていれば SSO で自動ログインされ、フォームは出ない——
  // フォームの再表示こそが「両方のセッションが終わった」ことの観測になる
  await page.getByRole("button", { name: "ログアウト" }).click();
  await page.waitForURL(/\/realms\/greenfield\//);
  await expect(page.locator("#username")).toBeVisible();
  await context.close();
});
