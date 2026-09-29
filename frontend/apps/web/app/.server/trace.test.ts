// newTraceContext の検証(#156)。API 側 core/middleware の Correlate と同じ規則:
// 正しい受信値は trace-id と sampled を引き継ぎ span は新規、壊れた値は捨てて採番。
import { test } from "node:test";
import assert from "node:assert/strict";
import { newTraceContext } from "@greenfield/api-core/trace";

const re = /^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$/;

test("受信値が無ければ採番する", () => {
  const tp = newTraceContext(null);
  const m = re.exec(tp);
  assert.ok(m, tp);
  assert.notEqual(m![1], "0".repeat(32));
  assert.equal(m![3], "01");
});

test("正しい受信値は trace-id と sampled を引き継ぎ、span は新規採番する", () => {
  const incoming = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01";
  const tp = newTraceContext(incoming);
  const m = re.exec(tp)!;
  assert.equal(m[1], "4bf92f3577b34da6a3ce929d0e0e4736");
  assert.notEqual(m[2], "00f067aa0ba902b7"); // 受信 span は親。流用すると親子が潰れる
  assert.equal(m[3], "01");
});

test("壊れた値は捨てて採番し直す", () => {
  for (const bad of [
    "garbage",
    "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // 未知バージョン
    "00-zzzz2f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // hexでない
    "00-00000000000000000000000000000000-00f067aa0ba902b7-01", // all-zero
    "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
  ]) {
    const tp = newTraceContext(bad);
    const m = re.exec(tp);
    assert.ok(m, `${bad} -> ${tp}`);
    assert.notEqual(m![1], "4bf92f3577b34da6a3ce929d0e0e4736", bad);
  }
});

test("同じ受信値から作った2つの context は span が異なる", () => {
  const incoming = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01";
  assert.notEqual(newTraceContext(incoming), newTraceContext(incoming));
});
