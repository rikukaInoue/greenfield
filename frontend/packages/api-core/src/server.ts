// SSR サーバー用の API クライアント共通層。トークン注入・X-Request-Id・Idempotency-Key・
// ステップアップ検知を担い、ブラウザにトークンを出さない前提で使う。
import createClient, { type Client, type Middleware } from "openapi-fetch";
import { randomUUID } from "node:crypto";

// Problem は RFC 9457 + code の応答本文。
export type Problem = {
  code: string;
  status?: number;
  title?: string;
  detail?: string;
  errors?: { location?: string; message?: string }[] | null;
};

// ApiError は API がエラーを返したことを表す。
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly problem: Problem,
  ) {
    super(problem.detail ?? problem.title ?? problem.code);
    this.name = "ApiError";
  }
}

// StepUpRequired は insufficient_user_authentication を受けたことを表す。呼び手は再認証へ誘導する。
export class StepUpRequired extends Error {
  constructor(
    readonly acrValues?: string,
    readonly maxAge?: number,
  ) {
    super("step-up authentication required");
    this.name = "StepUpRequired";
  }
}

export type ServerClientOptions = {
  baseUrl: string;
  // accessToken は呼び出しごとに評価される。
  accessToken: () => string | undefined;
  requestId?: string;
};

const mutating = new Set(["POST", "PUT", "PATCH", "DELETE"]);

// createServerClient は SSR の loader / action から使う型付きクライアントを返す。
export function createServerClient<Paths extends {}>(opts: ServerClientOptions): Client<Paths> {
  const client = createClient<Paths>({ baseUrl: opts.baseUrl });
  const mw: Middleware = {
    onRequest({ request }) {
      const token = opts.accessToken();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      request.headers.set("X-Request-Id", opts.requestId ?? randomUUID());
      if (mutating.has(request.method) && !request.headers.has("Idempotency-Key")) {
        request.headers.set("Idempotency-Key", randomUUID());
      }
      return request;
    },
    onResponse({ response }) {
      if (response.status !== 401) return;
      const challenge = parseBearerChallenge(response.headers.get("WWW-Authenticate"));
      if (challenge.error === "insufficient_user_authentication") {
        const maxAge = challenge.max_age ? Number(challenge.max_age) : undefined;
        throw new StepUpRequired(challenge.acr_values, maxAge);
      }
    },
  };
  client.use(mw);
  return client;
}

type Result<T> = { data?: T; error?: unknown; response: Response };

// unwrap は成功時の本文を返し、失敗時は ApiError を投げる。
export async function unwrap<T>(p: Promise<Result<T>>): Promise<T> {
  const { data, error, response } = await p;
  if (response.ok) return data as T;
  const problem: Problem =
    error && typeof error === "object" && "code" in error
      ? (error as Problem)
      : { code: "http." + response.status, detail: response.statusText };
  throw new ApiError(response.status, problem);
}

// parseBearerChallenge は WWW-Authenticate: Bearer の auth-param を取り出す。
export function parseBearerChallenge(header: string | null): Record<string, string> {
  const params: Record<string, string> = {};
  if (!header || !/^bearer\b/i.test(header)) return params;
  for (const m of header.slice(6).matchAll(/([\w-]+)\s*=\s*(?:"([^"]*)"|([^\s,]+))/g)) {
    params[m[1]] = m[2] ?? m[3];
  }
  return params;
}
