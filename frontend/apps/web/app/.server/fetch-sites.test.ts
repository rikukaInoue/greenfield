// #186-5: loader/action からの SSRF を「fetch 呼び出し箇所の固定」で防ぐ。
//
// loader はサーバで動くので、ユーザ入力由来の URL を fetch すると SSRF になる。
// 「宛先が全て設定（env.ts）か API 応答由来であること」を毎回目視するのは続かないので、
// fetch の呼び出し箇所そのものを許可リストにする。新しい fetch を書くとこのテストが
// 落ち、「その URL はどこから来たか」のレビューを1回だけ強制する。通すには下の
// 許可リストへ追記する（追記の diff がレビューの記録になる）。
import assert from "node:assert/strict";
import { test } from "node:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const appDir = join(fileURLToPath(new URL(".", import.meta.url)), "..");

// 許可済みの fetch 呼び出し箇所: "相対パス: 件数"。宛先の由来をコメントで示す。
const allowed: Record<string, number> = {
  // OIDC ディスカバリとトークン交換。宛先は env.oidc.issuer（設定由来）
  ".server/oidc.ts": 2,
  // 作成 action（自サイトの resource route、相対 URL）と、署名付き URL への PUT
  // （photo API の応答由来。ユーザ入力ではない）。どちらもブラウザ側で実行される
  "routes/photos.new.tsx": 2,
};

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return walk(p);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.ts$/.test(name) ? [p] : [];
  });
}

test("fetch の呼び出し箇所が許可リストと一致する", () => {
  const found: Record<string, number> = {};
  const files = walk(appDir);
  assert.ok(files.length > 5, `走査対象が ${files.length} 件しかない。走査が壊れている`);
  for (const f of files) {
    const n = (readFileSync(f, "utf8").match(/\bfetch\(/g) ?? []).length;
    if (n > 0) found[relative(appDir, f)] = n;
  }
  assert.deepEqual(
    found,
    allowed,
    "fetch の呼び出し箇所が変わった。宛先がユーザ入力由来でないこと（設定 or API 応答由来）を確認して許可リストを更新する",
  );
});
