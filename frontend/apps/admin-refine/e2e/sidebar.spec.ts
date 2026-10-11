import { expect, test, type Page } from "@playwright/test";

// サイドバー付きレイアウト(#277 追加仕様): メニューは refine の resources から導出され、
// 現在地がハイライトされ、上部に operator 名 / AAL / ログアウトがある。
const OPERATOR = process.env.E2E_OPERATOR ?? "admin-refine-e2e";
const SHOTS = process.env.E2E_SHOTS;

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

// 現在地の項目は bg-sidebar-primary(レジストリ Sidebar の選択スタイル)が付く
function menuLink(page: Page, label: string) {
  return page.locator('[data-sidebar="content"]').getByRole("link", { name: label });
}

test("メニューは resources から出て、現在地がハイライトされる。ヘッダに operator と AAL", async ({ page }) => {
  await login(page, OPERATOR, 1);
  const sidebar = page.locator('[data-sidebar="content"]');
  await expect(sidebar).toBeVisible();
  await expect(sidebar.getByRole("link")).toHaveText(["写真", "アカウント", "機材"]);
  await expect(menuLink(page, "写真")).toHaveClass(/bg-sidebar-primary/);
  await expect(page.getByRole("banner")).toContainText(OPERATOR);
  await expect(page.getByRole("banner")).toContainText("AAL1");
  await expect(page.getByRole("banner").getByRole("button", { name: "ログアウト" })).toBeVisible();
  await expect(page.getByRole("table").getByRole("row").nth(1)).toBeVisible({ timeout: 10_000 });
  await shot(page, "07-sidebar-list");

  await menuLink(page, "アカウント").click();
  await expect(page).toHaveURL(/\/accounts$/);
  await expect(menuLink(page, "アカウント")).toHaveClass(/bg-sidebar-primary/);
  await expect(menuLink(page, "写真")).not.toHaveClass(/bg-sidebar-primary/);
  await expect(page.getByRole("heading", { name: "アカウント" })).toBeVisible();
  await shot(page, "07-sidebar-accounts");

  await menuLink(page, "機材").click();
  await expect(page).toHaveURL(/\/gear$/);
  await expect(page.getByText("準備中")).toBeVisible();
  await shot(page, "07-sidebar-gear");
});

test("アカウント画面: subject 入力 → 確認 → AAL1 で step-up → AAL2 → 完遂", async ({ page }) => {
  await login(page, OPERATOR, 1);
  await menuLink(page, "アカウント").click();
  const victim = `nobody-${Date.now().toString(36)}`;
  await page.getByLabel("削除する subject").fill(victim);
  await page.getByRole("button", { name: "アカウントと全投稿を削除" }).click();
  await expect(page.getByRole("alertdialog")).toContainText(victim);
  await page.getByRole("button", { name: "削除する" }).click();
  await page.waitForURL(/\/login\?.*aal=2/);
  await expect(page.getByText("再認証が必要です")).toBeVisible();
  await shot(page, "07-sidebar-stepup");

  await page.getByLabel("ユーザー名").fill(OPERATOR);
  await page.getByLabel("認証保証レベル").selectOption("2");
  await page.getByRole("button", { name: "ログイン" }).click();
  await expect(page).toHaveURL(/\/accounts\?resume_subject=/);
  await expect(page.getByRole("banner")).toContainText("AAL2");
  await expect(page.getByText("再認証しました")).toBeVisible();
  await expect(page.getByLabel("削除する subject")).toHaveValue(victim);
  await shot(page, "07-sidebar-accounts-resume");
  await page.getByRole("button", { name: "アカウントと全投稿を削除" }).click();
  await page.getByRole("button", { name: "削除する" }).click();
  await expect(page.getByText(`${victim} を削除しました`)).toBeVisible();
  await shot(page, "07-sidebar-accounts-deleted");
});

test("ログイン前はサイドバーを出さない", async ({ page }) => {
  await page.goto("/login");
  await expect(page.locator('[data-sidebar="content"]')).toHaveCount(0);
});
