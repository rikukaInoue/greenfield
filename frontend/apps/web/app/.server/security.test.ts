// #186-3: SSR のセキュリティヘッダの中身を固定する。
// 「付けたつもり」と「付いている」を分けるヘッダ実在の検査は e2e（security-headers.spec）側。
// ここはディレクティブの構成が黙って痩せないことを固定する。
import assert from "node:assert/strict";
import { test } from "node:test";

import { securityHeaders } from "./security.ts";

test("CSP は骨格のディレクティブを全て持つ", () => {
  const csp = securityHeaders["Content-Security-Policy"];
  assert.ok(csp, "Content-Security-Policy が無い");
  for (const directive of [
    "default-src 'self'",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ]) {
    assert.ok(csp.includes(directive), `CSP に ${directive} が無い: ${csp}`);
  }
});

test("画像ストレージの origin は img-src と connect-src の両方に居る（表示と PUT）", () => {
  const csp = securityHeaders["Content-Security-Policy"];
  const img = csp.split("; ").find((d) => d.startsWith("img-src"));
  const connect = csp.split("; ").find((d) => d.startsWith("connect-src"));
  assert.ok(img?.includes("http://"), `img-src にストレージ origin が無い: ${img}`);
  assert.ok(connect?.includes("http://"), `connect-src にストレージ origin が無い: ${connect}`);
});

test("CSP 以外の防御ヘッダ", () => {
  assert.equal(securityHeaders["X-Content-Type-Options"], "nosniff");
  assert.equal(securityHeaders["Referrer-Policy"], "same-origin");
  assert.equal(securityHeaders["X-Frame-Options"], "DENY");
  assert.ok(securityHeaders["Permissions-Policy"].includes("camera=()"));
});

// script-src の 'unsafe-inline' は React Router のハイドレーションのための既知の妥協。
// nonce 化で外した日にこのテストが気づかせる（外したらこの assert を反転する）
test("script-src はハイドレーションのため 'unsafe-inline' を許している（既知の妥協）", () => {
  const csp = securityHeaders["Content-Security-Policy"];
  assert.ok(csp.includes("script-src 'self' 'unsafe-inline'"));
});
