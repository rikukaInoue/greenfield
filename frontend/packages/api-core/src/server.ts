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
// parameter property を使わないのは node --test の型ストリップ(strip-only)が
// その構文を読めず、このファイルを import するテストが書けなくなるため(#156)。
export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem;
  constructor(status: number, problem: Problem) {
    super(problem.detail ?? problem.title ?? problem.code);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }
}

// StepUpRequired は insufficient_user_authentication を受けたことを表す。呼び手は再認証へ誘導する。
export class StepUpRequired extends Error {
  readonly acrValues?: string;
  readonly maxAge?: number;
  constructor(acrValues?: string, maxAge?: number) {
    super("step-up authentication required");
    this.name = "StepUpRequired";
    this.acrValues = acrValues;
    this.maxAge = maxAge;
  }
}

export type ServerClientOptions = {
  baseUrl: string;
  // accessToken は呼び出しごとに評価される。
  accessToken: () => string | undefined;
  requestId?: string;
  // traceparent は W3C Trace Context。newTraceContext() で作った値を渡すと
  // 全 API 呼び出しに付き、1画面の呼び出し群が同じ trace-id で繋がる(#156)。
  // API 側(core/middleware.Correlate)は受信した span を親として自分の span を新規採番する。
  traceparent?: string;
};

export { newTraceContext } from "./trace.ts";

const mutating = new Set(["POST", "PUT", "PATCH", "DELETE"]);

// createServerClient は SSR の loader / action から使う型付きクライアントを返す。
export function createServerClient<Paths extends {}>(opts: ServerClientOptions): Client<Paths> {
  const client = createClient<Paths>({ baseUrl: opts.baseUrl });
  const mw: Middleware = {
    onRequest({ request }) {
      const token = opts.accessToken();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      request.headers.set("X-Request-Id", opts.requestId ?? randomUUID());
      if (opts.traceparent) request.headers.set("traceparent", opts.traceparent);
      // **このヘッダは現時点で誰も読んでいない**（サーバ側に Idempotency-Key を読む Go コードは
      // 無く、`core/httpclient` も未実装。#138）。重複排除としてはまだ機能していない。
      //
      // 自動生成はフォールバックで、呼び手が安定キーを渡せばそれを尊重する（has の判定）。
      // ただし**リトライを入れるならこのフォールバックでは足りない**: 試行ごとに新しい UUID に
      // なるので、同一操作の再送が別要求として扱われる。安定キーは「操作の同一性」を知っている
      // 呼び手側でしか作れないため、リトライを入れる時は呼び手からの明示指定を必須にする。
      // いまリトライ処理が無いので事象としては起きていない（顕在化は 4.1 以降）。
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
