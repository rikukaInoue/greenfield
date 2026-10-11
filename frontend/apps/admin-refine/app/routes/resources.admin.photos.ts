import { unwrap } from "@greenfield/api-core/server";
import type { Route } from "./+types/resources.admin.photos";
import { adminClientContext } from "../context";
import { toJsonError } from "../.server/resources";

// loader は全ユーザーの写真一覧を admin API から取って返す（BFF）。
// refine の dataProvider.getList がここを叩く。ブラウザが知るのはこの相対 URL だけ。
export async function loader({ request, context }: Route.LoaderArgs) {
  const url = new URL(request.url);
  const ownerId = url.searchParams.get("owner_id") ?? undefined;
  const limitRaw = Number(url.searchParams.get("limit") ?? 50);
  const limit = Number.isInteger(limitRaw) && limitRaw >= 1 && limitRaw <= 200 ? limitRaw : 50;
  const api = context.get(adminClientContext);
  try {
    const out = await unwrap(api.GET("/photos", { params: { query: { ...(ownerId ? { owner_id: ownerId } : {}), limit } } }));
    return Response.json({ photos: out.photos ?? [] });
  } catch (err) {
    return toJsonError(err);
  }
}
