import { createServerClient } from "@greenfield/api-core/server";
import type { paths } from "@greenfield/gear-api";
import { env } from "./env";

export type GearClient = ReturnType<typeof createGearClient>;

// createGearClient は gear external API のクライアントを返す。
// photo と同じ trace / request-id を渡すことで、1画面の合成呼び出しが同じ trace で繋がる。
export function createGearClient(accessToken: string, requestId: string, traceparent?: string) {
  return createServerClient<paths>({ baseUrl: env.gearApiUrl, accessToken: () => accessToken, requestId, traceparent });
}
