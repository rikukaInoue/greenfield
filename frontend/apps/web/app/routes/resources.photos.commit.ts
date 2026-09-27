import { unwrap } from "@greenfield/api-core/server";
import type { Route } from "./+types/resources.photos.commit";
import { photoClientContext } from "../context";
import { toJsonError } from "../.server/resources";

// action はアップロード完了を photo へ確定させる。
export async function action({ params, context }: Route.ActionArgs) {
  const id = Number(params.id);
  if (!Number.isSafeInteger(id) || id < 1) return Response.json({ code: "not_found" }, { status: 404 });
  try {
    const photo = await unwrap(context.get(photoClientContext).POST("/v2/photos/{id}:commit", { params: { path: { id } } }));
    return Response.json({ id: photo.id });
  } catch (err) {
    return toJsonError(err);
  }
}
