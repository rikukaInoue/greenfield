// createServerClient が外向きリクエストに付けるヘッダの検証(#156)。
// fetch を差し替えて実際の Request を捕まえる(ミドルウェアの単体でなく合成結果を見る)。
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { createServerClient } from "@greenfield/api-core/server";

const captured: Request[] = [];
const realFetch = globalThis.fetch;
globalThis.fetch = async (input: any, init?: any) => {
  captured.push(new Request(input, init));
  return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
};
after(() => { globalThis.fetch = realFetch; });

test("traceparent と X-Request-Id が外向きリクエストに付く", async () => {
  const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-a1b2c3d4e5f60718-01";
  const client = createServerClient<any>({
    baseUrl: "http://example.test",
    accessToken: () => "tok",
    requestId: "req-1",
    traceparent: tp,
  });
  await client.GET("/photos" as any, {} as any);
  await client.POST("/photos" as any, { body: {} } as any);

  assert.equal(captured.length, 2);
  for (const req of captured) {
    assert.equal(req.headers.get("traceparent"), tp, "全呼び出しが同じ trace context を持つ");
    assert.equal(req.headers.get("X-Request-Id"), "req-1");
  }
  // 変更系のみ Idempotency-Key(既存挙動が壊れていないこと)
  assert.equal(captured[0].headers.get("Idempotency-Key"), null);
  assert.ok(captured[1].headers.get("Idempotency-Key"));
});

test("traceparent 未指定なら付けない(既存挙動)", async () => {
  captured.length = 0;
  const client = createServerClient<any>({ baseUrl: "http://example.test", accessToken: () => undefined });
  await client.GET("/x" as any, {} as any);
  assert.equal(captured[0].headers.get("traceparent"), null);
});
