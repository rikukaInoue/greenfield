import type { AuthProvider, DataProvider, HttpError } from "@refinedev/core";

// このファイルはブラウザで動く。ここに書いた URL が、ブラウザが知る全て。
// admin API の所在もトークンも知らず、自サイトの resource route（BFF）だけを叩く。

// toHttpError は BFF の JSON エラー応答を refine の HttpError に変える。
async function toHttpError(res: Response): Promise<HttpError> {
  const body = (await res.json().catch(() => ({}))) as { code?: string; detail?: string };
  return { statusCode: res.status, message: body.detail ?? res.statusText, errors: undefined, ...body };
}

export const dataProvider: DataProvider = {
  getApiUrl: () => "/resources/admin",
  async getList({ resource, filters, pagination }) {
    if (resource !== "photos") throw new Error(`unknown resource ${resource}`);
    const q = new URLSearchParams();
    for (const f of filters ?? []) {
      if ("field" in f && f.field === "owner_id" && f.value) q.set("owner_id", String(f.value));
    }
    const limit = pagination?.pageSize ?? 50;
    q.set("limit", String(Math.min(Math.max(limit, 1), 200)));
    const res = await fetch(`/resources/admin/photos?${q}`, { headers: { Accept: "application/json" } });
    if (!res.ok) throw await toHttpError(res);
    const body = (await res.json()) as { photos: { id: number }[] };
    // admin API はページングを持たない（limit のみ）。total は取れた件数
    return { data: body.photos as never, total: body.photos.length };
  },
  // アカウント削除は REST の delete ではなく :delete の verb 付きコマンドなので custom で呼ぶ
  async custom({ url, method }) {
    const res = await fetch(url, { method: method.toUpperCase(), headers: { Accept: "application/json" } });
    if (!res.ok) throw await toHttpError(res);
    return { data: (await res.json()) as never };
  },
  getOne: () => Promise.reject(new Error("not implemented")),
  create: () => Promise.reject(new Error("not implemented")),
  update: () => Promise.reject(new Error("not implemented")),
  deleteOne: () => Promise.reject(new Error("not implemented")),
};

// stepUpLogin は再認証（AAL2）へ送る URL。戻り先に進行中の操作を載せ、戻ったら完遂させる。
export function stepUpLogin(returnTo: string): string {
  return `/login?${new URLSearchParams({ returnTo, aal: "2" })}`;
}

// authProvider: ログイン自体はサーバ側 action（/login）が担うので、ここは「エラーをどう扱うか」だけ。
// 401 auth.step_up_required → ステップアップへ。401 unauthenticated → ログインへ。
export function createAuthProvider(currentPath: () => string): AuthProvider {
  return {
    login: async () => ({ success: false, error: { name: "login", message: "サーバ側の /login を使う" } }),
    logout: async () => ({ success: true, redirectTo: "/login" }),
    check: async () => ({ authenticated: true }), // 未ログインはサーバの middleware が /login へ飛ばす
    onError: async (error: HttpError & { code?: string }) => {
      // ステップアップはここで遷移させない。進行中の操作(resume_* の query)を戻り先に載せられるのは
      // 操作を知っている画面側だけで、ここが currentPath() で遷移すると query の無い戻り先と
      // 競合して勝った方が残る(実測: /accounts?resume_subject= が /accounts に化けた)
      if (error?.statusCode === 401 && error.code === "auth.step_up_required") {
        return { logout: false };
      }
      if (error?.statusCode === 401) {
        return { redirectTo: `/login?${new URLSearchParams({ returnTo: currentPath() })}`, logout: false };
      }
      return {};
    },
    getIdentity: async () => null,
  };
}
