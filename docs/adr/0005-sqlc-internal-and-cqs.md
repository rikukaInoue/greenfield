# 0005. sqlc 生成型は各層の `internal/` に閉じ、CQS で分割する

## 背景

`conventions/internal-02` は「sqlcが生成した型はRepository実装の内部に閉じ、ドメインEntityや外部APIへ
露出させない」と定める。これを規約の文言だけで守るのは難しい（import すれば通ってしまう）。

また CQS により、コマンド側（Entity の復元・保存）とクエリ側（Read Model 直行）は別経路である。
同じ生成パッケージを両方から使うと、読み側から更新用クエリが見えてしまう。

## 決定

出力先を Go の `internal` 配下にし、コンパイラに守らせる。あわせて sqlc の設定を2つに分ける。

| 入力 | 出力 | 使う層 |
|---|---|---|
| `db/queries/repository/` | `repository/internal/sqlcgen` | コマンド側（Entity 経由） |
| `db/queries/readmodel/` | `readmodel/internal/sqlcgen` | クエリ側（Read Model 直行） |

`usecase` から `repository/internal/sqlcgen` を import するとビルドが通らない（実測で確認済み）。

## 影響

sqlc の設定ブロックがサービスごとに2つになる。スキーマ（`db/schema.sql`）は共通で1つ。

## 還流

`conventions/internal-02` に「生成先を `<layer>/internal/sqlcgen` にすればコンパイラが守る」を追記。
CQS の経路分離を sqlc の設定単位にも反映させる形も併せて記載する。
