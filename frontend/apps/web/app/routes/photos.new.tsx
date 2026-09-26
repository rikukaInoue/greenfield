import { imageContentTypes } from "@greenfield/photo-api";
import { data, Form, redirect, useNavigation } from "react-router";
import type { Route } from "./+types/photos.new";

type Problem = { code?: string; detail?: string };

async function postForm<T>(url: string, body: Record<string, string>): Promise<T> {
  const res = await fetch(url, { method: "POST", body: new URLSearchParams(body) });
  const json = await res.json().catch(() => ({}));
  if (res.status === 401) throw redirect(`/login?${new URLSearchParams({ returnTo: "/photos/new" })}`);
  if (!res.ok) throw new Error((json as Problem).detail ?? `投稿に失敗しました（${res.status}）`);
  return json as T;
}

// clientAction は作成 → 署名URLへ直接 PUT → commit の3段で投稿する。画像は SSR を経由しない。
export async function clientAction({ request }: Route.ClientActionArgs) {
  const form = await request.formData();
  const file = form.get("file");
  if (!(file instanceof File) || file.size === 0) return data({ error: "画像を選んでください" });
  if (!imageContentTypes.includes(file.type as never)) return data({ error: "JPEG / PNG / WebP / AVIF のみ投稿できます" });

  try {
    const created = await postForm<{ id: number; uploadUrl: string; contentType: string }>("/resources/photos", {
      caption: String(form.get("caption") ?? ""),
      visibility: String(form.get("visibility") ?? "private"),
      content_type: file.type,
    });
    const put = await fetch(created.uploadUrl, {
      method: "PUT",
      headers: { "Content-Type": created.contentType },
      body: file,
    });
    if (!put.ok) throw new Error(`画像のアップロードに失敗しました（${put.status}）`);
    await postForm(`/resources/photos/${created.id}/commit`, {});
    return redirect(`/photos/${created.id}?fresh=1`);
  } catch (err) {
    if (err instanceof Response) throw err;
    return data({ error: err instanceof Error ? err.message : "投稿に失敗しました" });
  }
}

export function meta() {
  return [{ title: "投稿 | greenfield photos" }];
}

export default function NewPhoto({ actionData }: Route.ComponentProps) {
  const submitting = useNavigation().state === "submitting";
  const field = "mt-1 w-full rounded border border-stone-300 bg-white px-3 py-2 dark:border-stone-700 dark:bg-stone-900";
  return (
    <div className="mx-auto max-w-lg">
      <h1 className="text-xl font-semibold">写真を投稿</h1>
      <Form method="post" encType="multipart/form-data" className="mt-6 space-y-4">
        <label className="block">
          <span className="text-sm">画像</span>
          <input type="file" name="file" required accept={imageContentTypes.join(",")} className={field} />
        </label>
        <label className="block">
          <span className="text-sm">キャプション</span>
          <textarea name="caption" maxLength={1000} rows={3} className={field} />
        </label>
        <fieldset className="flex gap-4 text-sm">
          <label className="flex items-center gap-1">
            <input type="radio" name="visibility" value="private" defaultChecked /> 非公開
          </label>
          <label className="flex items-center gap-1">
            <input type="radio" name="visibility" value="public" /> 公開
          </label>
        </fieldset>
        {actionData?.error && <p className="text-sm text-red-600">{actionData.error}</p>}
        <button
          disabled={submitting}
          className="rounded bg-stone-900 px-4 py-2 text-white disabled:opacity-50 dark:bg-stone-100 dark:text-stone-900"
        >
          {submitting ? "アップロード中…" : "投稿する"}
        </button>
      </Form>
    </div>
  );
}
