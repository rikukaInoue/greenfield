# migrations（expand / contract 二系統）

- `expand/`: 追加系のみ（ADD COLUMN NULL可/DEFAULT、CREATE TABLE、CREATE INDEX）。機能リリースに同梱し、デプロイ前ステップで `gear migrate expand` として適用する
- `contract/`: 削除・変更系（DROP COLUMN、NOT NULL化、型変更）。キューに積み、後続リリースで `gear migrate contract` として明示実行する。前提: sqlc 再生成で参照ゼロ + 対応する `release.` フラグが削除済み
- 履歴テーブルは系統ごとに分離（`gear_migrations_expand` / `gear_migrations_contract`）
- 入れないもの: CREATE DATABASE、ユーザー・権限（`deploy/compose/mysql/init/` が持つ）
- ファイル名は golang-migrate の `NNNNNN_name.{up,down}.sql`。down は形式上維持、本番では使わない
