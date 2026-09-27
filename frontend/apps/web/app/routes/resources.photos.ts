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
  try {
    const created = await unwrap(
      api.POST("/v2/photos", {
        body: {
          caption: String(form.get("caption") ?? ""),
          content_type: contentType as ImageContentType,
          visibility: form.get("visibility") === "public" ? "public" : "private",
        },
      }),
    );
    return Response.json({ id: created.id, uploadUrl: created.upload_url, contentType });
  } catch (err) {
    return toJsonError(err);
  }
}
