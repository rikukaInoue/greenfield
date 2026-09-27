// 監査 D-3 の回帰テスト。Go 側 core/runtimeenv と同じ表を固定する。
import assert from "node:assert/strict";
import { test } from "node:test";

import { current, isDevelopment, type Kind } from "./runtimeenv.ts";

const cases: Array<{ env: string | undefined; want: Kind }> = [
  { env: "dev", want: "dev" },
  { env: "development", want: "dev" },
  { env: "local", want: "dev" },
  { env: "test", want: "test" },
  { env: "ci", want: "ci" },
  // 未設定・綴り違い・本番は全て prod（拒否リストだと prd が通り抜けた）
  { env: undefined, want: "prod" },
  { env: "", want: "prod" },
  { env: "prd", want: "prod" },
  { env: "production", want: "prod" },
  { env: "prod", want: "prod" },
  { env: "staging", want: "prod" },
];

for (const c of cases) {
  test(`ENV=${c.env ?? "(未設定)"} は ${c.want}`, () => {
    assert.equal(current({ ENV: c.env }), c.want);
    assert.equal(isDevelopment({ ENV: c.env }), c.want !== "prod");
  });
}

// D-3 の本体。NODE_ENV を判定材料にしていたため、ENV=production でも
// NODE_ENV 未設定なら開発用の既定値が通っていた。
test("NODE_ENV は判定に影響しない", () => {
  for (const nodeEnv of [undefined, "development", "production", "test"]) {
    assert.equal(isDevelopment({ ENV: undefined, NODE_ENV: nodeEnv }), false, `ENV 未設定 / NODE_ENV=${nodeEnv}`);
    assert.equal(isDevelopment({ ENV: "production", NODE_ENV: nodeEnv }), false, `ENV=production / NODE_ENV=${nodeEnv}`);
    assert.equal(isDevelopment({ ENV: "dev", NODE_ENV: nodeEnv }), true, `ENV=dev / NODE_ENV=${nodeEnv}`);
  }
});
