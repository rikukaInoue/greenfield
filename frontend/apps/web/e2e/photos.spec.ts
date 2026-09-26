import { expect, test } from "@playwright/test";
import { login, upload, uniqueSubject } from "./helpers";

test("未ログインはログインへ戻り先付きで飛ばされる", async ({ page }) => {
  await page.goto("/photos/new");
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fphotos%2Fnew$/);
});

test("投稿: 作成 → 署名URLへ直接 PUT → commit で、直後の詳細に画像が出る", async ({ page }) => {
  const storage: string[] = [];
  page.on("request", (r) => {
    if (new URL(r.url()).port === "9000") storage.push(`${r.method()} ${new URL(r.url()).pathname}`);
  });

  await login(page, uniqueSubject());
  const caption = `朝の光 ${Date.now()}`;
  await upload(page, caption);

  await expect(page.getByText(caption)).toBeVisible();
  await expect(page.getByText("非公開")).toBeVisible();
  const img = page.getByRole("img", { name: caption });
  await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBe(1);
  // 画像はブラウザからストレージへ直接届き、SSR を経由しない
  expect(storage.some((s) => s.startsWith("PUT /photo-images/"))).toBe(true);
});

test("公開すると公開フィルタの一覧に出る", async ({ page }) => {
  await login(page, uniqueSubject());
  const caption = `公開テスト ${Date.now()}`;
  const id = await upload(page, caption);

  await page.getByRole("button", { name: "公開する" }).click();
  await expect(page.getByText("公開", { exact: true })).toBeVisible();

  await page.goto("/?visibility=public");
  await expect(page.locator(`a[href="/photos/${id}"]`)).toBeVisible();
  await page.goto("/?visibility=private");
  await expect(page.locator(`a[href="/photos/${id}"]`)).toHaveCount(0);
});

test("他人の写真は一覧に出ず、詳細は開けない", async ({ browser }) => {
  const owner = await browser.newPage();
  await login(owner, uniqueSubject("owner"));
  const id = await upload(owner, `非公開 ${Date.now()}`);

  const other = await browser.newPage();
  await login(other, uniqueSubject("other"));
  await expect(other.locator(`a[href="/photos/${id}"]`)).toHaveCount(0);
  const res = await other.goto(`/photos/${id}`);
  expect([403, 404]).toContain(res?.status());
});

test("対応外の画像形式は送信前に弾く", async ({ page }) => {
  await login(page, uniqueSubject());
  await page.goto("/photos/new");
  await page.getByLabel("画像").setInputFiles({ name: "a.gif", mimeType: "image/gif", buffer: Buffer.from("GIF89a") });
  await page.getByRole("button", { name: "投稿する" }).click();
  await expect(page.getByText("JPEG / PNG / WebP / AVIF のみ投稿できます")).toBeVisible();
  await expect(page).toHaveURL(/\/photos\/new$/);
});
