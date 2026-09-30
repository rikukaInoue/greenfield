// SSR レスポンスのセキュリティヘッダ（#186）。
// API と違い SSR は**ブラウザが実行する HTML** を返すので、CSP が実際に意味を持つのはこちら。
//
// script-src の 'unsafe-inline' は React Router のハイドレーション（ストリーミングの
// インライン script）のための妥協で、nonce 化すれば外せる（entry.server の自作が要る）。
// それでも default-src 'self' / frame-ancestors 'none' / form-action 'self' は
// 外部への送信・埋め込みを止めるので、無いのとは別物。
import { env } from "./env.ts";

// 画像は署名付き URL でオブジェクトストレージから直接読む（表示 <img> と
// アップロードの PUT。#17）。その origin だけを img-src / connect-src に足す
const imageOrigin = env.imageOrigin;

const csp = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline'",
  "style-src 'self' 'unsafe-inline'",
  `img-src 'self' ${imageOrigin}`,
  `connect-src 'self' ${imageOrigin}`,
  "font-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

// securityHeaders は全 SSR レスポンスに付けるヘッダ。
export const securityHeaders: Record<string, string> = {
  "Content-Security-Policy": csp,
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "same-origin",
  // frame-ancestors と重複するが、CSP を解さない古い経路への保険として残す
  "X-Frame-Options": "DENY",
};
