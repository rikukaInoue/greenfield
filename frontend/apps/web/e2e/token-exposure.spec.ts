import { expect, test } from "@playwright/test";
import { login, upload, uniqueSubject } from "./helpers";

// #17: SSR はトークンをブラウザに出さない。CORS は全リスナーで閉じている。

// sessionToken はセッション Cookie（署名付き base64 JSON）からアクセストークンを取り出す。
function sessionToken(cookieValue: string): string {
  const signed = decodeURIComponent(cookieValue);
  const payload = signed.slice(0, signed.lastIndexOf("."));
  return JSON.parse(Buffer.from(payload, "base64").toString("utf8")).accessToken;
}

test("トークンは httpOnly Cookie の中だけにあり、JS・HTML・通信に出ない", async ({ page, context }) => {
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
  const token = sessionToken(session!.value);
  expect(token).toMatch(/^dev\./);

  const exposed = await page.evaluate(() => ({
    cookie: document.cookie,
    local: JSON.stringify({ ...localStorage }),
    session: JSON.stringify({ ...sessionStorage }),
    html: document.documentElement.outerHTML,
  }));
  expect(exposed.cookie).not.toContain("__gf_session");
  for (const v of [exposed.local, exposed.session, exposed.html]) expect(v).not.toContain(token);
  for (const b of bodies) expect(b).not.toContain(token);

  expect(outgoing.filter((r) => r.authorization)).toEqual([]);
  expect(outgoing.filter((r) => /:(8080|8081|8082)\//.test(r.url))).toEqual([]);
});

for (const port of [8080, 8081, 8082]) {
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
