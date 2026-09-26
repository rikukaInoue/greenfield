# greenfield

プロダクション設計（マルチドメインAPIモノレポ + Yii段階移行）の **greenfield検証ビルド**。
移行を持たない新規プロダクトとして、Goバックエンドと認可基盤を個人のプライベート環境で構築し、設計の主張を実証する試作（prototype）。

主役は **CI/CDのサービス単位分離** と **フィーチャーフラグによるオンラインマイグレーション**。
Phase 0〜6 は完全ローカル（クラウド費用ゼロ）、AWS は Phase 7 のみ・短期間の使い捨て。

## ドキュメント

設計書一式は [`docs/`](docs/README.md) にある。

| ファイル | 内容 |
|---|---|
| [docs/01-scope.md](docs/01-scope.md) | 目的・検証しないこと・成功条件・還流物 |
| [docs/02-architecture.md](docs/02-architecture.md) | リポジトリ構成・compose構成・全体像 |
| [docs/03-platform.md](docs/03-platform.md) | 認可基盤（authz / OpenFGA）の構築仕様とKeycloak設定 |
| [docs/04-milestones.md](docs/04-milestones.md) | 検証チェックリスト（成功条件の契約） |
| [docs/05-roadmap.md](docs/05-roadmap.md) | 段階検証ロードマップ（Phase 0〜7） |
| [docs/conventions/](docs/conventions/README.md) | プロダクション版と同一のコード規約（移行記述のみ除去） |

## 進捗管理

- **Milestone** = Phase 0〜7
- **Issue** = ロードマップの各ステージ（`kind:stage`）と検証チェックリストの各項目（`kind:check`）。チェック項目 #N は Issue #N と番号を揃えてある
- 証跡は `docs/verification-log.md` に追記する

## 開発

ツールとタスクは `mise.toml` に集約している（`mise run` でタスク一覧から選べる）。

```
mise install                 # Go / Node / pnpm（mise.toml）
mise run build:ws            # ワークスペースで全モジュールをビルド（日常用）
mise run check               # fmt + build + vet + test を GOWORK=off で並列実行（CI相当。境界チェックはこちらでしか効かない）
mise run infra:up            # mysql + RustFS + flagd を起動し、バケット作成とマイグレーションまで済ませる
mise run db:up               # mysql:8.4 のみ（database/ユーザー/GRANT は deploy/compose/mysql/init）
mise run s3:up               # RustFS のみ（バケットは dev/s3admin が作る）
mise run migrate             # 各サービスの expand を適用（<name> migrate expand|contract|status）
mise run schema:dump         # マイグレーション全適用後の dump で db/schema.sql（sqlc の入力）を更新
mise run sqlc                # sqlc generate（生成型は各層の internal/sqlcgen に閉じる）
mise run api                 # huma の型から api/<service>/<listener>.openapi.json を生成
mise run api:breaking        # base に対する破壊的変更を検出（メジャー未更新なら失敗）
mise run schema:check        # db/schema.sql がマイグレーションと一致しているか（CI）
mise run run                 # 全サービスを1プロセスで起動（Tier 1: photo :8080/:8081/:8082）
mise run token -- --user alice   # 開発用トークン（認証は全リスナーで有効。トークンなしは 401）
mise run flags               # フラグの現在値（定義は deploy/compose/flagd/flags.json を git 管理）
mise run scaffold <name>     # 新サービスの骨格を生成（ポートは +10 で採番）
mise run web:dev             # frontend/ の SSR（http://localhost:5173、/login で開発用ログイン）
mise run web:gen             # api/ の OpenAPI から TS の型を再生成
mise run web:check           # 生成型の一致 + typecheck + build（CI）
```
