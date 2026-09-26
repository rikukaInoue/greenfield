# 0010. フラグは入口で1回評価して ctx に積む

## 背景

`conventions/internal-08` は「判定はリクエスト入口のミドルウェアで1回行い、結果をctxに積む
（ハンドラ・usecaseは評価済みの値を読むだけ。1リクエスト内で値が揺れない）」と定める。
OpenFeature SDK をそのまま usecase から呼ぶと、同一リクエスト内で評価が複数回走り、
フラグの配信反映（Eventual、数秒の窓）に当たった場合に値が揺れる。

## 決定

`core/flags` に評価をまとめる。

- サービスは評価するフラグを `flags.Set` として宣言する（名前 + 既定値）。既定値は
  フラグ基盤が停止していても安全な側（既存動作）にする
- `Evaluator.Middleware()` が宣言された全フラグを1回評価し、`map[string]bool` を ctx へ積む。
  認証ミドルウェアより後に置く（ターゲティングキーに Principal を使うため）
- ハンドラ・usecase は `flags.Bool(ctx, name)` しか使わない。未評価の ctx では false（安全側）
- 評価が失敗しても宣言した既定値へ倒し、警告ログだけ出す

自前の差し込み口（interface）は作らない。OpenFeature が標準 interface を持つので、
プロバイダ（flagd / Unleash）の差し替えは合成ルートの1行で済む。

**追記（[ADR 0013](0013-flags-evaluate-in-process.md)）**: 当初はプロバイダを flagd の既定
（`rpc` = flagd 側で評価）で登録し、無名の `SetProvider` を使っていた。評価の場所を
AppConfig / GrowthBook と揃えるため `in-process`（定義を同期してプロセス内で評価）へ変更し、
登録も `SetNamedProvider` へ変えた。無名の登録はプロセスグローバルで、
1プロセスに複数サービスが載る場合に互いを上書きする。

## 影響

- フラグを増やすときは `flagSet` への宣言が必要。宣言を忘れると `flags.Bool` が false を返す
  （安全側に倒れるが、意図した ON にならない）。命名を定数にして宣言と参照を揃えている
- テストは `flags.WithValues(ctx, ...)` で値を注入する。flagd を立てずに ON / OFF 両方を通せる
- 割合展開（1.5d）のターゲティングキーは `Principal.Subject`。テナントの概念はまだない

## 還流

`conventions/internal-08` の「入口で1回評価」を実装形として書き足す。宣言（名前 + 既定値）を
サービスが持ち、ミドルウェアがまとめて評価する形が、規約の4つの規約
（既定値は安全側 / 入口で1回 / フラグ同士を依存させない / 配信は Eventual）をそのまま満たす。

> 追記: SSR 側での評価はやめた。表示に必要な判定はサービスが「できること」として API で返す（[ADR 0014](0014-ssr-does-not-evaluate-flags.md)）。
