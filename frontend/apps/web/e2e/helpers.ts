import { expect, type Page } from "@playwright/test";

// uniqueSubject はテストごとに他と混ざらないユーザー名を返す。
export function uniqueSubject(prefix = "e2e") {
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
}

export async function login(page: Page, subject: string, aal: 1 | 2 = 1) {
  await page.goto("/login");
  await page.getByLabel("ユーザー名").fill(subject);
  await page.getByLabel("認証保証レベル").selectOption(String(aal));
  await page.getByRole("button", { name: "ログイン" }).click();
  await expect(page.getByRole("banner")).toContainText(subject);
}

// png は 1x1 の PNG。
export const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
  "base64",
);

export async function upload(page: Page, caption: string, visibility: "private" | "public" = "private") {
  await page.goto("/photos/new");
  await page.getByLabel("画像").setInputFiles({ name: "p.png", mimeType: "image/png", buffer: png });
  await page.getByLabel("キャプション").fill(caption);
  await page.getByLabel(visibility === "public" ? "公開" : "非公開", { exact: true }).check();
  await page.getByRole("button", { name: "投稿する" }).click();
  await page.waitForURL(/\/photos\/\d+\?fresh=1$/);
  return Number(new URL(page.url()).pathname.split("/").pop());
}
