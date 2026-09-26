import { unwrap } from "@greenfield/api-core/server";
import type { Visibility } from "@greenfield/photo-api";
import { Link, NavLink } from "react-router";
import type { Route } from "./+types/photos";
import { photoClientContext } from "../context";
import { toRouteError } from "../.server/errors";
import { formatDate, PhotoImage, VisibilityBadge } from "../components";

function parseVisibility(v: string | null): Visibility | undefined {
  return v === "public" || v === "private" ? v : undefined;
}

export async function loader({ request, context }: Route.LoaderArgs) {
  const visibility = parseVisibility(new URL(request.url).searchParams.get("visibility"));
  const api = context.get(photoClientContext);
  try {
    const body = await unwrap(api.GET("/photos", { params: { query: { visibility, limit: 50 } } }));
    return { photos: body.photos ?? [], visibility, canCreate: body.can_create };
  } catch (err) {
    toRouteError(err, request);
  }
}

const filters = [
  { label: "すべて", to: "/" },
  { label: "公開", to: "/?visibility=public" },
  { label: "非公開", to: "/?visibility=private" },
];

export default function Photos({ loaderData }: Route.ComponentProps) {
  const { photos, visibility, canCreate } = loaderData;
  return (
    <div>
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">写真</h1>
        {canCreate ? (
          <Link to="/photos/new" className="rounded bg-stone-900 px-3 py-1.5 text-sm text-white dark:bg-stone-100 dark:text-stone-900">
            投稿する
          </Link>
        ) : (
          <span className="text-sm text-stone-500">投稿を一時停止中</span>
        )}
      </div>
      <nav className="mt-4 flex gap-2 text-sm">
        {filters.map((f) => {
          const active = (f.to.split("=")[1] ?? undefined) === visibility;
          return (
            <NavLink
              key={f.to}
              to={f.to}
              className={`rounded-full border px-3 py-1 ${active ? "border-stone-900 dark:border-stone-100" : "border-stone-300 text-stone-500 dark:border-stone-700"}`}
            >
              {f.label}
            </NavLink>
          );
        })}
      </nav>
      {photos.length === 0 ? (
        <p className="mt-10 text-center text-stone-500">写真はまだありません。</p>
      ) : (
        <ul className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3">
          {photos.map((p) => (
            <li key={p.id}>
              <Link to={`/photos/${p.id}`} className="block overflow-hidden rounded-lg border border-stone-200 dark:border-stone-800">
                <PhotoImage photo={p} className="aspect-square w-full" />
                <div className="space-y-1 p-3">
                  <p className="truncate text-sm">{p.caption || "（キャプションなし）"}</p>
                  <div className="flex items-center justify-between text-xs text-stone-500">
                    <span>{formatDate(p.created_at)}</span>
                    <VisibilityBadge visibility={p.visibility} />
                  </div>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
