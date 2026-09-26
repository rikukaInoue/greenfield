// devtoken は core/authz/devtoken と同じ形式の擬似トークンを組み立てる。
export type DevClaims = { sub: string; aal?: number; auth_time?: number };

export function mintDevToken(c: DevClaims): string {
  const claims = { sub: c.sub, aal: c.aal ?? 1, auth_time: c.auth_time ?? Math.floor(Date.now() / 1000) };
  return "dev." + Buffer.from(JSON.stringify(claims)).toString("base64url");
}
