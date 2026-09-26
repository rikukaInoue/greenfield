import { ApiError, unwrap } from "@greenfield/api-core/server";
import { data, Form, redirect, useNavigation } from "react-router";
import type { Route } from "./+types/photos.detail";
import { photoClientContext } from "../context";
import { toRouteError } from "../.server/errors";
import { formatDate, PhotoImage, VisibilityBadge } from "../components";

function photoId(raw: string): number {
  const id = Number(raw);
  if (!Number.isSafeInteger(id) || id < 1) throw data("Not Found", { status: 404 });
  return id;
}

export async function loader({ request, params, context }: Route.LoaderArgs) {
  const id = photoId(params.id);
  // fresh は自分の書き込み直後に Read Model の遅れを踏まないためのヒント。
  const fresh = new URL(request.url).searchParams.get("fresh") === "1";
  const api = context.get(photoClientContext);
  try {
    const photo = await unwrap(api.GET("/photos/{id}", { params: { path: { id }, query: { fresh } } }));
    return { photo };
  } catch (err) {
    toRouteError(err, request);
  }
}

export async function action({ request, params, context }: Route.ActionArgs) {
  const id = photoId(params.id);
  const form = await request.formData();
  const api = context.get(photoClientContext);
  if (form.get("intent") !== "publish") throw data("Bad Request", { status: 400 });
  try {
    await unwrap(api.POST("/photos/{id}:publish", { params: { path: { id } } }));
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      return data({ error: err.problem.detail ?? "公開できない状態です" }, { status: 409 });
    }
    toRouteError(err, request);
  }
  return redirect(`/photos/${id}?fresh=1`);
}

export function meta({ loaderData }: Route.MetaArgs) {
  return [{ title: `${loaderData?.photo.caption || "写真"} | greenfield photos` }];
}

export default function PhotoDetail({ loaderData, actionData }: Route.ComponentProps) {
  const { photo } = loaderData;
  const publishing = useNavigation().formData?.get("intent") === "publish";
  return (
    <article className="grid gap-6 md:grid-cols-[3fr_2fr]">
      <PhotoImage photo={photo} className="w-full rounded-lg" />
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <VisibilityBadge visibility={photo.visibility} />
          <span className="text-sm text-stone-500">{formatDate(photo.created_at)}</span>
        </div>
        <p className="whitespace-pre-wrap">{photo.caption || "（キャプションなし）"}</p>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-stone-500">投稿者</dt>
          <dd>{photo.owner_id}</dd>
          {photo.gear_name && (
            <>
              <dt className="text-stone-500">機材</dt>
              <dd>{photo.gear_name}</dd>
            </>
          )}
          {photo.size_bytes != null && (
            <>
              <dt className="text-stone-500">サイズ</dt>
              <dd>{(photo.size_bytes / 1024).toFixed(1)} KiB</dd>
            </>
          )}
        </dl>
        {photo.visibility === "private" && (
          <Form method="post">
            <button
              name="intent"
              value="publish"
              disabled={publishing}
              className="rounded bg-emerald-700 px-3 py-1.5 text-sm text-white disabled:opacity-50"
            >
              {publishing ? "公開中…" : "公開する"}
            </button>
          </Form>
        )}
        {actionData?.error && <p className="text-sm text-red-600">{actionData.error}</p>}
      </div>
    </article>
  );
}
