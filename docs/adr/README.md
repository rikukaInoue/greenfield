# ADR（このビルドで下した設計判断）

`docs/` 配下の役割分担:

| 置き場所 | 内容 |
|---|---|
| `01-scope.md` 〜 `05-roadmap.md` | このビルド全体の設計と計画 |
| `conventions/` | プロダクション版のコード規約のコピー。**ここでは直さない**（還流してから再コピー） |
| `adr/`（本ディレクトリ） | 実装中に下した判断とその理由。規約から逸脱した箇所を含む |
| `verification-log.md` | 検証チェックリストの実験の証跡（時系列・追記専用） |

コード側のコメントには理由を書かない。「何をするか・呼び手が守るべきこと」だけを godoc に書き、
理由はここに置く。

## 書き方

1ファイル1判断。`NNNN-kebab-case.md` で通番。節は **背景 / 決定 / 影響 / 還流**。
規約（`conventions/`）と食い違う決定には「還流」節に、プロダクション版へ持ち帰るべき内容を書く。

## 一覧

| # | 判断 | 還流 |
|---|---|---|
| [0001](0001-router-chi.md) | ルータは chi（Echo は `:verb` と両立しない） | 要 |
| [0002](0002-handler-internalapi.md) | internal リスナーのパッケージ名は `internalapi` | 要 |
| [0003](0003-app-composition-root.md) | `app/` を合成ルートにし、ハンドラは Deps を受ける | 要 |
| [0004](0004-localauthz-separate-store.md) | 擬似ReBAC のタプルはサービスDBと別に置く | 不要 |
| [0005](0005-sqlc-internal-and-cqs.md) | sqlc 生成型は各層の `internal/` に閉じ、CQS で分割する | 要 |
| [0006](0006-ci-gowork-off.md) | モジュール境界の検証は `GOWORK=off` でしか効かない | 要 |
| [0007](0007-spec-version-before-implementation.md) | 実装前に切った契約のバージョンは 1.0.0 にしない | 要 |
| [0008](0008-object-storage-rustfs.md) | ローカルのオブジェクトストレージは RustFS（LocalStack S3 は使わない） | 要 |
| [0009](0009-presigned-upload.md) | 画像は署名付きURLでアップロードし pending → ready で確定する | 要 |
| [0010](0010-feature-flag-evaluation.md) | フラグは入口で1回評価して ctx に積む | 要 |
| [0011](0011-flag-provider-on-aws.md) | AWS では AppConfig 用の OpenFeature プロバイダを自作する | 要 |
| [0012](0012-eventual-publish-inside-atomic.md) | `Eventual.Publish` は `Atomic.Do` の中で呼ぶ | 要 |
| [0014](0014-ssr-does-not-evaluate-flags.md) | SSR はフラグを評価せず、判定結果を「できること」として API から受け取る | 要 |
