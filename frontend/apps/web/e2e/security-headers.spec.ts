import { expect, test } from "@playwright/test";
import { login, uniqueSubject } from "./helpers";

// #186-3: SSR レスポンスのセキュリティヘッダ。API と違い SSR はブラウザが実行する
// HTML を返すので、CSP が実際に意味を持つのはこちら側。
// #186-4: 状態を変える GET が無いこと。Cookie は sameSite=Lax でクロスサイト POST には
// 付かないが、Lax はトップレベル GET を通すので、GET で状態が変わる経路だけが穴になる。

test("SSR レスポンスに防御ヘッダが付く（未認証ページ）", async ({ page }) => {
  const res = await page.goto("/login");
  const h = res!.headers();
  expect(h["content-security-policy"]).toContain("default-src 'self'");
  expect(h["content-security-policy"]).toContain("frame-ancestors 'none'");
  expect(h["content-security-policy"]).toContain("form-action 'self'");
  expect(h["x-content-type-options"]).toBe("nosniff");
  expect(h["referrer-policy"]).toBe("same-origin");
  expect(h["x-frame-options"]).toBe("DENY");
});

test("SSR レスポンスに防御ヘッダが付く（認証済みページ）", async ({ page }) => {
  await login(page, uniqueSubject());
  const res = await page.goto("/");
  expect(res!.headers()["content-security-policy"]).toContain("frame-ancestors 'none'");
  expect(res!.headers()["x-content-type-options"]).toBe("nosniff");
});

test("CSP はページの実挙動を壊していない（違反レポートゼロ）", async ({ page }) => {
  const violations: string[] = [];
  page.on("console", (m) => {
    if (m.text().includes("Content Security Policy")) violations.push(m.text());
  });
  await login(page, uniqueSubject());
  await page.goto("/");
  await page.goto("/photos/new");
  expect(violations).toEqual([]);
});

test("GET /logout ではログアウトしない（Lax Cookie はトップレベル GET を通すため）", async ({ page }) => {
  await login(page, uniqueSubject());
  // 悪意あるサイトの <a href="https://…/logout"> を踏まされた形
  await page.goto("/logout");
  await expect(page).not.toHaveURL(/login/);
  // セッションは生きている: 認証必須のトップへ行ってもログインへ落ちない
  await page.goto("/");
  await expect(page).not.toHaveURL(/login/);
});

test("resource route への GET は副作用にならない（action のみ）", async ({ page }) => {
  await login(page, uniqueSubject());
  // 写真の作成は POST の action だけ。loader が無いので react-router が GET を
  // エラーにする（「loader を書いた覚えがないのに GET が通る」形に戻らないことの固定）
  const res = await page.request.get("/resources/photos");
  expect(res.status()).toBeGreaterThanOrEqual(400);
});
