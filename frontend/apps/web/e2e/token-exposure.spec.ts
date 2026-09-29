import { expect, test } from "@playwright/test";
import { login, upload, uniqueSubject } from "./helpers";

// #17: SSR はトークンをブラウザに出さない。CORS は全リスナーで閉じている。
//
// トークンは**サーバー側セッションストア**にあり、Cookie はストアのセッションIDだけを運ぶ。
// 以前は署名 Cookie にトークンが base64 で載っていた（httpOnly でも復号すれば見えた）。

// cookiePayload は署名 Cookie の中身（base64 JSON）を復号する。
// 攻撃者が Cookie 値を手に入れた場合に読める情報がこれの全て。
function cookiePayload(cookieValue: string): string {
  const signed = decodeURIComponent(cookieValue);
  const payload = signed.slice(0, signed.lastIndexOf("."));
  return Buffer.from(payload, "base64").toString("utf8");
}

// tokenLike はトークンに見える文字列。devtoken（dev. 接頭辞）と JWT（eyJ… の3分割）の両方を拾う。
const tokenLike = /dev\.[A-Za-z0-9+/=_-]{16,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/;

test("トークンは Cookie を復号しても出てこない（サーバー側ストアのみ）", async ({ page, context }) => {
  const outgoing: { url: string; authorization?: string }[] = [];
  page.on("request", (r) => outgoing.push({ url: r.url(), authorization: r.headers()["authorization"] }));
  const bodies: string[] = [];
  page.on("response", async (r) => {
    const type = r.headers()["content-type"] ?? "";
    if (/html|json|javascript|x-component|octet-stream|text\//.test(type)) bodies.push(await r.text().catch(() => ""));
  });

  await login(page, uniqueSubject());
  const id = await upload(page, `露出確認 ${Date.now()}`);
  await page.goto("/");
  await page.goto(`/photos/${id}`);

  const session = (await context.cookies()).find((c) => c.name === "__gf_session");
  expect(session).toBeDefined();
  expect(session!.httpOnly).toBe(true);
  expect(session!.sameSite).toBe("Lax");
  // Cookie の中身はセッションIDだけ。accessToken もトークン様の文字列も入っていない
  const payload = cookiePayload(session!.value);
  expect(payload).not.toContain("accessToken");
  expect(payload).not.toMatch(tokenLike);

  const exposed = await page.evaluate(() => ({
    cookie: document.cookie,
    local: JSON.stringify({ ...localStorage }),
    session: JSON.stringify({ ...sessionStorage }),
    html: document.documentElement.outerHTML,
  }));
  expect(exposed.cookie).not.toContain("__gf_session"); // httpOnly なので JS から見えない
  for (const v of [exposed.local, exposed.session, exposed.html]) expect(v).not.toMatch(tokenLike);
  for (const b of bodies) expect(b).not.toMatch(tokenLike);

  expect(outgoing.filter((r) => r.authorization)).toEqual([]);
  expect(outgoing.filter((r) => /:(8080|8081|8082|8090|8091|8092)\//.test(r.url))).toEqual([]);
});

for (const port of [8080, 8081, 8082, 8090, 8091, 8092]) {
  test(`ブラウザから API :${port} を直接叩けない（CORS 閉）`, async ({ page }) => {
    await page.goto("/login");
    const result = await page.evaluate(async (p) => {
      try {
        await fetch(`http://localhost:${p}/photos`, { headers: { Authorization: "Bearer dev.x" } });
        return "reachable";
      } catch {
        return "blocked";
      }
    }, port);
    expect(result).toBe("blocked");
  });
}
