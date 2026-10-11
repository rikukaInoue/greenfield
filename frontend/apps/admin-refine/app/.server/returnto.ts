// safeReturnTo は戻り先を同一オリジンの相対パスに限定する（オープンリダイレクト対策。web と同じ）。
export function safeReturnTo(raw: string | null | undefined, base: string | URL): string {
  if (!raw) return "/";
  if (!raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/";
  try {
    const u = new URL(raw, base);
    const b = new URL(base);
    if (u.origin !== b.origin) return "/";
    return u.pathname + u.search;
  } catch {
    return "/";
  }
}
