import { expect, test } from "@playwright/test";
import { login, upload, uniqueSubject } from "./helpers";

// #186-1: ハイドレーション payload への漏れ。loader の返り値は HTML に直列化されて
// ブラウザへ出る。トークンをサーバ側ストアに置いた努力（check #17）は、loader が
// 返してしまえば無効になる。ZAP からは 200 が返るだけで何も見えない。
//
// トークン形状そのものは token-exposure.spec が見ている。ここで見るのはその外側:
// セッションID・トークン風のフィールド名・内部ホスト名・他人のリソース。

// cookiePayload は署名 Cookie の中身（base64 JSON）。セッションIDはここに居る。
function cookiePayload(cookieValue: string): string {
  const signed = decodeURIComponent(cookieValue);
  return Buffer.from(signed.slice(0, signed.lastIndexOf(".")), "base64").toString("utf8");
}

test("セッションID・トークン風フィールド・内部ポートが SSR 応答に出ない", async ({ page, context }) => {
  const bodies: string[] = [];
  page.on("response", async (r) => {
    const type = r.headers()["content-type"] ?? "";
    if (/html|json|javascript|x-component|text\//.test(type)) bodies.push(await r.text().catch(() => ""));
  });

  await login(page, uniqueSubject());
  const id = await upload(page, `漏れ確認 ${Date.now()}`);
  await page.goto("/");
  await page.goto(`/photos/${id}`);

  const session = (await context.cookies()).find((c) => c.name === "__gf_session");
  expect(session).toBeDefined();
  // ファイルストアのセッションID（Cookie payload の中身）が HTML/JS に出たら、
  // Cookie を盗まずともセッションを指名できてしまう
  const sessionId = JSON.parse(cookiePayload(session!.value)) as unknown;
  const idValues = (typeof sessionId === "object" && sessionId ? Object.values(sessionId) : [sessionId])
    .filter((v): v is string => typeof v === "string" && v.length >= 8);
  expect(idValues.length).toBeGreaterThan(0);

  expect(bodies.length).toBeGreaterThan(2);
  for (const b of bodies) {
    for (const idv of idValues) expect(b).not.toContain(idv);
    // フィールド名ごと直列化される形（loader がセッションの中身を返した形）
    expect(b).not.toContain("refreshToken");
    expect(b).not.toContain("accessToken");
    expect(b).not.toContain("idToken");
    // 内部リスナー・API のポート。ブラウザに知らせる正当な理由が無い
    expect(b).not.toMatch(/:(8080|8081|8082|8090|8091|8092)\//);
  }
});

test("他人のリソースはハイドレーション payload に混ざらない", async ({ browser }) => {
  // alice が非公開の写真を作る
  const alicePage = await (await browser.newContext()).newPage();
  await login(alicePage, uniqueSubject("alice"));
  const caption = `他人の非公開 ${Date.now()}`;
  const id = await upload(alicePage, caption);

  // bob が ID 直打ちで詳細を、次に一覧を見る
  const bobPage = await (await browser.newContext()).newPage();
  const bodies: string[] = [];
  bobPage.on("response", async (r) => {
    const type = r.headers()["content-type"] ?? "";
    if (/html|json|javascript|x-component|text\//.test(type)) bodies.push(await r.text().catch(() => ""));
  });
  await login(bobPage, uniqueSubject("bob"));
  await bobPage.goto(`/photos/${id}`);
  await bobPage.goto("/");

  expect(bodies.length).toBeGreaterThan(1);
  for (const b of bodies) expect(b).not.toContain(caption);
});
