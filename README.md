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

```
mise install                                  # Go（mise.toml）
make build-ws                                 # ワークスペースで全モジュールをビルド（日常用）
make build vet test                           # 各モジュールを GOWORK=off で単体検証（CI相当。境界チェックはこちらでしか効かない）
make run-allinone                             # 全サービスを1プロセスで起動（Tier 1: photo :8080/:8081/:8082）
go run ./dev/scaffold new-service <name>      # 新サービスの骨格を生成（ポートは +10 で採番）
```
