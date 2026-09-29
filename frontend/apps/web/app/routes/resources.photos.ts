import { unwrap } from "@greenfield/api-core/server";
import { imageContentTypes, type ImageContentType } from "@greenfield/photo-api";
import type { Route } from "./+types/resources.photos";
import { photoClientContext } from "../context";
import { toJsonError } from "../.server/resources";

// action は写真を pending_upload で作成し、ブラウザが直接 PUT する署名付きURLを返す。
export async function action({ request, context }: Route.ActionArgs) {
  const form = await request.formData();
  const contentType = String(form.get("content_type"));
  if (!imageContentTypes.includes(contentType as ImageContentType)) {
    return Response.json({ code: "validation_failed", detail: "対応していない画像形式です" }, { status: 422 });
  }
  const api = context.get(photoClientContext);
  const gearItemRaw = String(form.get("gear_item_id") ?? "");
  const gearItemId = /^[1-9]\d*$/.test(gearItemRaw) ? Number(gearItemRaw) : undefined;
  try {
    const created = await unwrap(
      api.POST("/v2/photos", {
        body: {
          caption: String(form.get("caption") ?? ""),
          content_type: contentType as ImageContentType,
          visibility: form.get("visibility") === "public" ? "public" : "private",
          // 紐付けは photo→gear の同期コマンドで確定する（4.4）。ここは値を渡すだけ
          ...(gearItemId ? { gear_item_id: gearItemId } : {}),
        },
      }),
    );
    return Response.json({ id: created.id, uploadUrl: created.upload_url, contentType, gearLinkStatus: created.gear_link_status ?? "" });
  } catch (err) {
    return toJsonError(err);
  }
}
