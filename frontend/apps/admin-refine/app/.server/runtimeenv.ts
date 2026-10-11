// runtimeenv は実行環境の種別を判定する（web の app/.server/runtimeenv.ts と同じ表。
// Go 側の core/runtimeenv と意味論を揃える。値の集合がずれると片側だけ fail-open になる）。
export type Kind = "dev" | "test" | "ci" | "prod";

const developmentKinds: readonly Kind[] = ["dev", "test", "ci"];

export function current(env: NodeJS.ProcessEnv = process.env): Kind {
  switch (env.ENV) {
    case "dev":
    case "development":
    case "local":
      return "dev";
    case "test":
      return "test";
    case "ci":
      return "ci";
    default:
      return "prod";
  }
}

export function isDevelopment(env: NodeJS.ProcessEnv = process.env): boolean {
  return developmentKinds.includes(current(env));
}

export function describe(env: NodeJS.ProcessEnv = process.env): string {
  return env.ENV ? env.ENV : "(未設定)";
}
