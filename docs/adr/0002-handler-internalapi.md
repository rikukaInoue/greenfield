# 0002. internal リスナーのパッケージ名は `internalapi`

## 背景

`conventions/internal-01` のツリーは `handler/internal/` を指定している。しかし Go の
`internal` ディレクトリ規則により、`handler/internal` は `handler/` の配下からしか import できない。
サービスの組み立ては `app/`（および `cmd/`）が行うため、そこから import できず配線できない。

## 決定

ディレクトリとパッケージ名を `handler/internalapi/` にする。

## 影響

規約のツリー図と実際のディレクトリ名が1箇所だけ食い違う。scaffold のテンプレートも `internalapi` で揃えた。

## 還流

`conventions/internal-01` の `handler/internal/` を `internalapi/` 等へ改名する提案。
Go の言語規則に起因するため、プロダクション版でも同じ問題が必ず起きる。
