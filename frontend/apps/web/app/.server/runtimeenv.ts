// runtimeenv は実行環境の種別を判定する。
// 開発用の実装（署名検証のないトークン、固定の資格情報）を本番へ持ち込まないための入口である。
//
// Go 側の core/runtimeenv と同じ意味論にすること。値の集合がずれると、
// 「同じガード」のつもりで片側だけ fail-open になる（監査 D-3 がまさにそれ）。
//
//   ENV              | Kind
//   -----------------+------
//   dev              | dev
//   development      | dev
//   local            | dev
//   test             | test
//   ci               | ci
//   上記以外・未設定 | prod   ← 既定は本番（fail-closed）
//
// NODE_ENV は見ない。react-router-serve も pnpm dev も設定しない経路があり、
// 起動方法によって有無が変わる値を安全側の判定材料にはできない。

export type Kind = "dev" | "test" | "ci" | "prod";

/** 開発用の実装を許す環境。許可リストにすることで ENV の未設定や綴り違いを通さない。 */
const developmentKinds: readonly Kind[] = ["dev", "test", "ci"];

/** current は ENV から種別を返す。既定は prod。 */
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
      // 未設定も含めて本番とみなす
      return "prod";
  }
}

/** isDevelopment は開発用の実装が許される環境かを返す。 */
export function isDevelopment(env: NodeJS.ProcessEnv = process.env): boolean {
  return developmentKinds.includes(current(env));
}

/** describe は診断メッセージ用に ENV の実値を返す。 */
export function describe(env: NodeJS.ProcessEnv = process.env): string {
  return env.ENV ? env.ENV : "(未設定)";
}
