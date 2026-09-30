import { expect, test } from "@playwright/test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

// #186-2: `.server` 境界がクライアントバンドルに漏れていないこと。
// app/.server/ は react-router が client bundle から外すが、間接 import では漏れうる。
// **ビルドが通っても漏れる形**なので、ビルド後の成果物（build/client/）を見る。
// test:e2e は `react-router build && playwright test` なので成果物は必ずある。

// サーバ側にしか存在しない文字列リテラル。minify でも識別子と違い文字列は残る。
const serverOnlyMarkers = [
  // env.ts の環境変数名と開発既定値
  "PHOTO_API_URL",
  "GEAR_API_URL",
  "SESSION_SECRET",
  "SSR_CLIENT_SECRET",
  "ssr-dev-secret",
  "dev-session-secret",
  "IMAGE_ORIGIN",
  // セッションストアの置き場（session.ts）
  ".data/sessions",
  // OIDC のサーバ側フロー（oidc.ts）
  "openid-configuration",
];

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

test("client バンドルにサーバ側の識別文字列が無い", () => {
  const clientDir = join(process.cwd(), "build", "client");
  const files = walk(clientDir).filter((f) => /\.(js|css|html|json)$/.test(f));
  expect(files.length).toBeGreaterThan(3);
  let bytes = 0;
  for (const f of files) {
    const body = readFileSync(f, "utf8");
    bytes += body.length;
    for (const marker of serverOnlyMarkers) {
      expect(body, `${f} に "${marker}" が含まれている（.server の内容が client へ漏れている）`).not.toContain(marker);
    }
  }
  // 空のバンドルを走査して緑になる形を防ぐ
  expect(bytes).toBeGreaterThan(10_000);
});
