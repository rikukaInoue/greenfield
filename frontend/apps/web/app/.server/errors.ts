import { ApiError, StepUpRequired } from "@greenfield/api-core/server";
import { data, redirect } from "react-router";

// toRouteError は API 呼び出しの失敗を loader / action から投げる Response へ変換する。
export function toRouteError(err: unknown, request: Request): never {
  const here = new URL(request.url);
  const returnTo = here.pathname + here.search;
  if (err instanceof StepUpRequired) {
    throw redirect(`/login?${new URLSearchParams({ returnTo, aal: "2" })}`);
  }
  if (err instanceof ApiError) {
    if (err.status === 401) throw redirect(`/login?${new URLSearchParams({ returnTo })}`);
    throw data(err.problem, { status: err.status });
  }
  throw err;
}
