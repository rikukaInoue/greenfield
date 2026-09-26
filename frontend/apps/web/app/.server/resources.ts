import { ApiError, StepUpRequired } from "@greenfield/api-core/server";

// toJsonError は resource route 用に API の失敗を JSON 応答へ変換する。
export function toJsonError(err: unknown): Response {
  if (err instanceof StepUpRequired) {
    return Response.json({ code: "auth.step_up_required", detail: "再認証が必要です" }, { status: 401 });
  }
  if (err instanceof ApiError) return Response.json(err.problem, { status: err.status });
  throw err;
}
