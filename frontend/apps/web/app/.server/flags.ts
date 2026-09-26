import { FlagdProvider } from "@openfeature/flagd-provider";
import { OpenFeature, ProviderStatus, type EvaluationContext } from "@openfeature/server-sdk";
import type { Viewer } from "../context";
import { env } from "./env";

// flagSet は SSR が評価するフラグの宣言。名前と既定値は API 側の宣言と揃え、表示と API の判定を一致させる。
export const flagSet = [{ name: "ops.photo_disable_uploads", default: false }] as const;

export type FlagName = (typeof flagSet)[number]["name"];
export type FlagValues = Record<FlagName, boolean>;

// provider の登録はプロセスで1回。開発時のモジュール再読込で二重に登録しない。
const registered = globalThis as { __gfFlagdRegistered?: boolean };
if (!registered.__gfFlagdRegistered) {
  registered.__gfFlagdRegistered = true;
  OpenFeature.setProvider(new FlagdProvider({ host: env.flagdHost, port: env.flagdPort, deadlineMs: 500 }));
}

const client = OpenFeature.getClient("web");

// evalBudgetMs は1リクエストでフラグ評価に使える上限。flagd の障害をページの遅延にしない。
const evalBudgetMs = 200;

const defaults = Object.fromEntries(flagSet.map((f) => [f.name, f.default])) as FlagValues;

// evaluateFlags は宣言された全フラグを評価する。provider が使えない・上限を超えた場合は宣言した既定値へ倒す。
export async function evaluateFlags(viewer: Viewer | null): Promise<FlagValues> {
  // 接続が切れた provider は評価のたびに再接続を待ち、deadlineMs が効かない（数秒かかる）。
  if (client.providerStatus !== ProviderStatus.READY && client.providerStatus !== ProviderStatus.STALE) {
    console.warn(`フラグ基盤が使えないので既定値を使う status=${client.providerStatus}`);
    return defaults;
  }
  // ターゲティングキーと属性は API 側（core/flags）と同じ値を使う。
  const ctx: EvaluationContext = viewer ? { targetingKey: viewer.subject, subject: viewer.subject, kind: 1 } : {};
  const evaluate = Promise.all(
    flagSet.map(async (f) => {
      const d = await client.getBooleanDetails(f.name, f.default, ctx);
      if (d.errorCode) console.warn(`フラグの評価に失敗したので既定値を使う flag=${f.name} err=${d.errorCode}`);
      return [f.name, d.errorCode ? f.default : d.value] as const;
    }),
  ).then((entries) => Object.fromEntries(entries) as FlagValues);
  let timer: NodeJS.Timeout | undefined;
  const timeout = new Promise<FlagValues>((resolve) => {
    timer = setTimeout(() => {
      console.warn(`フラグ評価が ${evalBudgetMs}ms を超えたので既定値を使う`);
      resolve(defaults);
    }, evalBudgetMs);
  });
  try {
    return await Promise.race([evaluate, timeout]);
  } finally {
    clearTimeout(timer);
  }
}
