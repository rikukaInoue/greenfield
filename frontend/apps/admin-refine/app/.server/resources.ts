import { ApiError, StepUpRequired } from "@greenfield/api-core/server";

// toJsonError は BFF（resource route）用に admin API の失敗を JSON 応答へ変換する。
// ステップアップは 401 + code で返し、ブラウザ側の authProvider が再認証へ誘導する。
// WWW-Authenticate の中身（acr_values 等）はサーバ側で解釈済みなので、そのまま転送しない。
export function toJsonError(err: unknown): Response {
  if (err instanceof StepUpRequired) {
    return Response.json(
      { code: "auth.step_up_required", detail: "この操作には再認証（AAL2）が必要です", acr_values: err.acrValues ?? "aal2" },
      { status: 401 },
    );
  }
  if (err instanceof ApiError) return Response.json(err.problem, { status: err.status });
  throw err;
}
