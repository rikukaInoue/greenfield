import { expect, test, type Page } from "@playwright/test";

// 前提: subject OPERATOR が localauthz で platform operator になっていること
// （docker exec greenfield-mysql mysql ... INSERT INTO localauthz.relation_tuples ...）。
// 操作対象のアカウントは存在しない subject を使い、本物のデータを消さない（削除 API は
// 対象が無くても 200 / deleted_photos=0 で返る）。
const OPERATOR = process.env.E2E_OPERATOR ?? "admin-refine-e2e";
const SHOTS = process.env.E2E_SHOTS; // 指定時のみスクリーンショットを保存

// tokenLike は web の token-exposure.spec と同じ正規表現。devtoken と JWT の両方を拾う
const tokenLike = /dev\.[A-Za-z0-9+/=_-]{16,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/;

function cookiePayload(cookieValue: string): string {
  const signed = decodeURIComponent(cookieValue);
  return Buffer.from(signed.slice(0, signed.lastIndexOf(".")), "base64").toString("utf8");
}

async function login(page: Page, subject: string, aal: 1 | 2 = 1) {
  await page.goto("/login");
  await page.getByLabel("ユーザー名").fill(subject);
  await page.getByLabel("認証保証レベル").selectOption(String(aal));
  await page.getByRole("button", { name: "ログイン" }).click();
  await expect(page.getByRole("banner")).toContainText(subject);
}

async function shot(page: Page, name: string) {
  if (SHOTS) await page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true });
}

test("一覧 → 削除(AAL1) → ステップアップ → 削除完遂。全応答にトークンが出ない", async ({ page, context }) => {
  const bodies: { url: string; body: string }[] = [];
  const outgoing: string[] = [];
  page.on("request", (r) => outgoing.push(r.url()));
  page.on("response", async (r) => {
    const type = r.headers()["content-type"] ?? "";
    if (/html|json|javascript|x-component|text\//.test(type)) bodies.push({ url: r.url(), body: await r.text().catch(() => "") });
  });

  await page.goto("/login");
  await shot(page, "01-login");
  await login(page, OPERATOR, 1);

  // 一覧: refine のフック → BFF → admin API。行が描かれるまで待つ
  await expect(page.getByRole("heading", { name: "全ユーザーの写真" })).toBeVisible();
  const table = page.getByRole("table");
  await expect(table).toBeVisible();
  await expect(table.getByRole("row").nth(1)).toBeVisible({ timeout: 10_000 });
  await shot(page, "02-list");

  // 削除(AAL1): 確認ダイアログ → 401 step_up_required → /login?aal=2 へ
  const victim = `nobody-${Date.now().toString(36)}`;
  await page.getByLabel("owner_id で絞る").fill(victim);
  await page.getByRole("button", { name: "絞り込む" }).click();
  // 存在しない owner なので一覧は空。削除は URL 直接ではなく一覧の行から行うのが本来だが、
  // 本物のアカウントを消さないために「再認証後の続行」経路から進める
  await page.goto(`/?resume_delete=${victim}`);
  await expect(page.getByText(`${victim} の削除を続けますか？`)).toBeVisible();
  await page.getByRole("button", { name: "削除を続ける" }).click();
  await page.waitForURL(/\/login\?.*aal=2/);
  await expect(page.getByText("再認証が必要です")).toBeVisible();
  await shot(page, "04-stepup-login");

  // AAL2 で再認証 → 戻り先(resume_delete 付き)へ → 続行で完遂
  await page.getByLabel("ユーザー名").fill(OPERATOR);
  await page.getByLabel("認証保証レベル").selectOption("2");
  await page.getByRole("button", { name: "ログイン" }).click();
  await expect(page.getByRole("banner")).toContainText("AAL2");
  await expect(page.getByText(`${victim} の削除を続けますか？`)).toBeVisible();
  await shot(page, "05-resume-after-stepup");
  await page.getByRole("button", { name: "削除を続ける" }).click();
  await expect(page.getByText(`${victim} を削除しました`)).toBeVisible();
  await shot(page, "06-deleted");

  // --- 非露出 ---
  const session = (await context.cookies()).find((c) => c.name === "__gf_admin_session");
  expect(session).toBeDefined();
  expect(session!.httpOnly).toBe(true);
  expect(session!.sameSite).toBe("Lax");
  const payload = cookiePayload(session!.value);
  expect(payload).not.toContain("accessToken");
  expect(payload).not.toMatch(tokenLike);

  const exposed = await page.evaluate(() => ({
    cookie: document.cookie,
    local: JSON.stringify({ ...localStorage }),
    session: JSON.stringify({ ...sessionStorage }),
    html: document.documentElement.outerHTML,
  }));
  expect(exposed.cookie).not.toContain("__gf_admin_session");
  for (const v of [exposed.local, exposed.session, exposed.html]) expect(v).not.toMatch(tokenLike);
  expect(bodies.length).toBeGreaterThan(5);
  for (const b of bodies) {
    expect(b.body, b.url).not.toMatch(tokenLike);
    expect(b.body, b.url).not.toMatch(/:(8080|8081|8082|18080|18081|18082)\//);
  }
  // ブラウザから出た要求は全て自サイト向け（admin API へ直接は飛ばない）
  expect(outgoing.filter((u) => !u.startsWith(page.url().split("/").slice(0, 3).join("/")))).toEqual([]);
});

test("確認ダイアログは一覧の行から開く", async ({ page }) => {
  await login(page, OPERATOR, 2);
  const firstDelete = page.getByRole("button", { name: /のアカウントを削除$/ }).first();
  await expect(firstDelete).toBeVisible({ timeout: 10_000 });
  await firstDelete.click();
  await expect(page.getByRole("alertdialog")).toContainText("アカウントと全投稿を削除します");
  await page.waitForTimeout(400); // 開くアニメーションの途中を撮らない（スクリーンショット用）
  await shot(page, "03-delete-dialog");
  await page.getByRole("button", { name: "やめる" }).click();
  await expect(page.getByRole("alertdialog")).toBeHidden();
});

test("GET /logout ではログアウトしない（Lax Cookie の穴を開けない）", async ({ page }) => {
  await login(page, OPERATOR, 1);
  await page.goto("/logout");
  await page.goto("/");
  await expect(page).not.toHaveURL(/login/);
});

test("未ログインの BFF は 401、画面はログインへ", async ({ page }) => {
  const res = await page.request.get("/resources/admin/photos");
  expect(res.status()).toBe(401);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login\?returnTo=/);
});
