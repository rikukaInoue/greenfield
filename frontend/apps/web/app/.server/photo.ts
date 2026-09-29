import { createServerClient } from "@greenfield/api-core/server";
import type { paths } from "@greenfield/photo-api";
import { env } from "./env";

export type PhotoClient = ReturnType<typeof createPhotoClient>;

// createPhotoClient は photo external API のクライアントを返す。
export function createPhotoClient(accessToken: string, requestId: string, traceparent?: string) {
  return createServerClient<paths>({ baseUrl: env.photoApiUrl, accessToken: () => accessToken, requestId, traceparent });
}
