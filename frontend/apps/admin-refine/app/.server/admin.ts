import { createServerClient } from "@greenfield/api-core/server";
import type { paths } from "@greenfield/photo-api/admin";
import { env } from "./env";

export type AdminClient = ReturnType<typeof createAdminClient>;

// createAdminClient は photo admin API のクライアントを返す。呼ぶのはサーバ側（BFF）のみ。
// 401 insufficient_user_authentication は api-core が StepUpRequired として投げる。
export function createAdminClient(accessToken: string, requestId: string) {
  return createServerClient<paths>({ baseUrl: env.photoAdminUrl, accessToken: () => accessToken, requestId });
}
