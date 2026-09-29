// W3C Trace Context の生成・引き継ぎ(#156)。server.ts から分けているのは、
// node --test の型ストリップが server.ts の parameter property 構文を読めず、
// テストから import できないため(このファイルは strip-only で通る構文に限定する)。

const traceparentRe = /^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$/;

// newTraceContext は SSR 1リクエスト分の traceparent を作る。
// 受信値が正しい形式なら trace-id と sampled を引き継ぎ、span-id は**必ず新規採番**する
// (受信 span はこの SSR 処理の親であり、そのまま流用すると親子が潰れる)。
// 壊れた値は黙って捨てて採番し直す。不正なヘッダで 500 を返すと、経路上の壊れた
// プロキシ1つで画面が落ちる(API 側 core/middleware.Correlate と同じ規則)。
export function newTraceContext(incoming?: string | null): string {
  const spanId = randomHex(8);
  const m = incoming ? traceparentRe.exec(incoming) : null;
  if (m && !/^0+$/.test(m[1]) && !/^0+$/.test(m[2])) {
    return `00-${m[1]}-${spanId}-${m[3]}`;
  }
  return `00-${randomHex(16)}-${spanId}-01`;
}

function randomHex(bytes: number): string {
  const buf = new Uint8Array(bytes);
  crypto.getRandomValues(buf);
  return Array.from(buf, (b) => b.toString(16).padStart(2, "0")).join("");
}
