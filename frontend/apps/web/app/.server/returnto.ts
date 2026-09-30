// safeReturnTo は returnTo パラメータを同一オリジンのパスへ正規化する。
// 外部オリジンに解決される値、および Location に載せるとオリジンを離れる値は "/" に落とす。
export function safeReturnTo(value: string | null | undefined, base: string | URL): string {
  if (!value) return "/";

  let origin: string;
  try {
    origin = new URL(base).origin;
  } catch {
    return "/";
  }

  let resolved: URL;
  try {
    resolved = new URL(value, origin);
  } catch {
    return "/";
  }
  if (resolved.origin !== origin) return "/";

  // pathname が "//host" の形だと Location ではプロトコル相対 URL として解釈される。
  // 返す値をもう一度解決し、ブラウザが同じ結論に至ることを確かめる。
  const path = resolved.pathname + resolved.search + resolved.hash;
  try {
    if (new URL(path, origin).origin !== origin) return "/";
  } catch {
    return "/";
  }
  return path;
}
