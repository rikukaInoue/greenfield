import { unwrap } from "@greenfield/api-core/server";
import type { Route } from "./+types/resources.admin.accounts.delete";
import { adminClientContext } from "../context";
import { toJsonError } from "../.server/resources";

// action はアカウントと全投稿を削除する（AAL2 要求。足りなければ 401 auth.step_up_required を返す）。
export async function action({ params, context }: Route.ActionArgs) {
  const api = context.get(adminClientContext);
  try {
    const out = await unwrap(api.POST("/accounts/{subject}:delete", { params: { path: { subject: params.subject } } }));
    return Response.json({ deleted_photos: out.deleted_photos });
  } catch (err) {
    return toJsonError(err);
  }
}
