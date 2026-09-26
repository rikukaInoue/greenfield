# 0001. ルータは chi（Echo は `:verb` と両立しない）

## 背景

`conventions/api-design.md` は2つのことを同時に定めている。

1. フレームワークは Echo + huma（`humaecho` アダプタ）
2. コマンドのURLは `:verb`（AIP-136 のカスタムメソッド。例 `POST /photos/{id}:publish`）

Echo のパスパラメータ構文は `:id` であり、huma が `/photos/{id}:publish` を Echo 向けに変換すると
`/photos/:id:publish` になる。Echo はこれを「`id:publish` という名前のパラメータ」として扱うため、
huma が求める `id` が取れない。

実測（huma v2.39.1、`POST /photos/42:publish`）:

| アダプタ | 結果 |
|---|---|
| `humaecho`（echo v4） | 422 `required path parameter is missing: path.id` |
| `humaecho`（echo v5） | 422 同上 |
| `humachi`（chi v5） | 200 `{"id":42}` |
| `humago`（net/http ServeMux） | panic `bad wildcard segment (must end with '}')` |

## 決定

`:verb` を維持し、ルータを **chi** にする。`core/httpapi` が chi の Mux を組み立てる。

`:verb` はAPI設計の中核（コマンドとクエリの経路分離、usecase と1対1のエンドポイント）であり、
ルータは差し替え可能な実装詳細である。優先順位は明らかに `:verb` が上。

## 影響

- ミドルウェアは元々 net/http 形式で書いていたため、`echo.WrapMiddleware` が不要になり配線は単純化した
- Echo への依存が消えた（`core/go.mod` から echo が落ちた）
- 将来 Echo に戻す場合は `:verb` を諦めるか、ルータ層でパスを書き換える必要がある

## 還流

`conventions/api-design.md` §3.1 の「Echo + humaecho」は `:verb` 規約と両立しない。
プロダクション版でどちらを採るかの判断が必要。上記の実測表をそのまま持ち帰る。
