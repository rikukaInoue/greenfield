# Project: greenfield

マルチドメインAPIモノレポ設計の検証ビルド。設計の正典は `docs/`（README → 01〜05 → conventions/ → adr/）。
このファイルは作業規律だけを持ち、設計内容は docs 側を読むこと。

## Build & Test

- `mise run check` — fmt / build / vet / test / lint / 生成物の一致（CI と同一）
- `mise run test` — テストのみ
- `mise run migrate` — ローカル DB を最新化（expand → contract）
- `mise run api` / `api:check` — OpenAPI の生成 / 一致検査
- `mise run run` — allinone で全サービス起動（開発の日常）
- モジュール単位の操作は `MODULES=./services/photo` のように絞る

## Critical Rules

- **並行エージェントが常態**。issue 着手前に open PR と issue コメントを確認し、
  着手宣言コメントを書いてから始める（CONTRIBUTING）。認証・認可系（Phase 3、
  Keycloak / OpenFGA / authz）は別エージェントの担当レーン
- 規約で禁じたものは機械でも止める。「文章だけの禁止は1行で静かに破られる」
- 検査・実測は「通った」ではなく「何を見た上で通ったか」まで言えること
  （空の結果と失敗を区別できない書き方をしない）
- .env は読み書きしない / rm -rf・force push は使わない
- サービス間で database を跨ぐ FK を張らない（CI の fk:check が止める）

## Task Execution Protocol

1. 変更したら `mise run check` を通してからコミットする
2. コンパクションが起きたら CLAUDE.md と docs/README.md を再読する
3. 測れることは測ってから主張する。推測で書かない
4. 大きな変更は小さなコミット単位に分割する

## Commit Rules

- 意味のある単位でコミットする。メッセージは「なぜ」を書く（何を、は diff で分かる）
- テストが通る状態でのみコミットする
- PR は CI green 確認後にセルフレビューしてからマージ（ユーザーの包括承認 2026-09-28。
  リリース・課金・削除を伴うものは毎回確認）
