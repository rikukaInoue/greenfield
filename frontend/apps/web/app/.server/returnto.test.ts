// #185 の回帰テスト。拒否リスト（前方一致）では止まらなかった形を固定する。
import assert from "node:assert/strict";
import { test } from "node:test";

import { safeReturnTo } from "./returnto.ts";

const BASE = "https://app.example";

const passthrough: Array<[string, string]> = [
  ["/photos", "/photos"],
  ["/photos?fresh=1", "/photos?fresh=1"],
  ["/photos/abc#section", "/photos/abc#section"],
  ["photos", "/photos"],
  ["https://app.example/photos", "/photos"],
  // 制御文字は URL パーサが落とす。同一オリジンのパスとして通る
  ["/\tevil.com", "/evil.com"],
  // %2f はパスの一部。デコードされないので外へ出ない
  ["/%2f%2fevil.com", "/%2f%2fevil.com"],
];

for (const [input, want] of passthrough) {
  test(`同一オリジンは通す: ${JSON.stringify(input)}`, () => {
    assert.equal(safeReturnTo(input, BASE), want);
  });
}

const rejected: Array<[string, string]> = [
  ["//evil.com", "プロトコル相対 URL"],
  ["/\\evil.com", "バックスラッシュ1本（special scheme では / と同じ扱い）"],
  ["/\\\\evil.com", "バックスラッシュ2本"],
  ["///evil.com", "スラッシュ3本"],
  ["https://app.example//evil.com", "同一オリジンだが pathname が //host になる"],
  ["https://evil.com/x", "別オリジン"],
  ["http://app.example/x", "スキームが違う"],
  ["javascript:alert(1)", "javascript スキーム"],
  ["data:text/html,<script>alert(1)</script>", "data スキーム"],
];

for (const [input, why] of rejected) {
  test(`外へ出る値は落とす: ${JSON.stringify(input)}（${why}）`, () => {
    assert.equal(safeReturnTo(input, BASE), "/");
  });
}

test("空・未指定は /", () => {
  assert.equal(safeReturnTo(null, BASE), "/");
  assert.equal(safeReturnTo(undefined, BASE), "/");
  assert.equal(safeReturnTo("", BASE), "/");
});

test("返した値は Location に載せても同一オリジンに留まる", () => {
  const inputs = [...passthrough.map(([i]) => i), ...rejected.map(([i]) => i), "", "/"];
  for (const input of inputs) {
    const out = safeReturnTo(input, BASE);
    assert.equal(
      new URL(out, BASE).origin,
      BASE,
      `${JSON.stringify(input)} -> ${JSON.stringify(out)} が外へ出た`,
    );
  }
});

test("base が壊れていれば /", () => {
  assert.equal(safeReturnTo("/photos", "not a url"), "/");
});
