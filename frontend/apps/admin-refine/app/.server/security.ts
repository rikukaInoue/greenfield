// SSR レスポンスのセキュリティヘッダ（web の security.ts と同じ形）。
//
// 比較の論点: antd は CSS-in-JS（@ant-design/cssinjs）で <style> をインライン注入するため
// style-src の 'unsafe-inline' は外せない。script-src の 'unsafe-inline' は React Router の
// ハイドレーション script のため（web と同じ妥協）。どちらも実測で確認してから書いた。
const csp = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "connect-src 'self'",
  "font-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

export const securityHeaders: Record<string, string> = {
  "Content-Security-Policy": csp,
  "X-Content-Type-Options": "nosniff",
  "Referrer-Policy": "same-origin",
  "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=()",
  "X-Frame-Options": "DENY",
};
