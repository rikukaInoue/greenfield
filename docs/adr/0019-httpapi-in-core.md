# 0019. huma + chi は `core/httpapi` に閉じて core に置く（監査 C-5 の記録）

## 背景

`internal-01` は core を「横断的関心事のみ。軽依存に保つ」と定める。一方で実物の
`core/go.mod` の direct require には `huma/v2` と `chi/v5` が居る。chi を選んだ判断は
ADR 0001 にあるが、**「huma + chi を core に置き、core の全利用者に持ち込む」という
判断そのものは無記録だった**（監査 C-5 の不一致1）。この ADR は既に行われている
その判断を、代替案と一緒に後追いで記録する。

## 決定

huma + chi は core に置く。ただし **`core/httpapi` パッケージに閉じる**。

- リスナーの組み立て（huma API・ルータ・認証/スコープ/基盤ミドルウェアの適用順）は
  全サービスで同一でなければならない。これはまさに「横断的関心事」であり、
  サービスごとにコピーすると適用順の揺れ（例: アクセスログが認証の内側に入る）が
  サービス差分として入り込む。#142 で基盤スタックを「選ばせず必ず積む」と決めた以上、
  積む場所は共有モジュールにしか置けない
- ルータに依存しない関心事は `httpapi` に**入れない**。`middleware`（net/http のみ）、
  `logger`（stdlib のみ）、`problem`（huma 依存だが応答形式の定義のみ）と分けてある。
  依存の重さはパッケージ単位で段差を付ける

## 代替案

- **`services/` 側に置く**: リスナーの組み立てがサービスごとに複製され、適用順を
  規約（文章）でしか守れなくなる。監査が繰り返し見つけている「文章だけの禁止は
  1行で静かに破られる」形になるため不採用
- **別モジュール（`core-http/` 等）に分割する**: 依存の分離としては最も綺麗だが、
  現状 core の利用者は photo / gear / dev の3つで、**全員が httpapi を使っている**。
  利用者が分かれていないのに境界だけ増やすのは早い。`*-client`（契約側）が core に
  依存していないことは確認済みで、huma / chi が契約モジュールへ漏れる経路は無い。
  httpapi を使わない core 利用者が現れた時に分割を再検討する

## 影響

- core の module graph に huma / chi が乗る。ビルド時間・脆弱性スキャンの対象が増える
- `dev`（allinone / genapi）は httpapi 経由で huma を使うので実質影響なし
- 差し替え（ルータ変更）は ADR 0001 の通り httpapi の中だけで済む

## テスト専用依存の扱い（ADR 0004 の条件の明文化）

`go-sql-driver/mysql` は `localauthz_test.go` だけが import するが、go.mod は
direct / indirect しか区別しないため direct require に現れる（ADR 0004 の訂正）。

**条件**: `_test.go` のみが import する依存は core に置いてよい。ただし
非テストコードから import した時点で「core の利用者全員に配る」判断になるため、
この ADR のような記録を要する。判別は `go mod why -m <module>` の経路に
`.test` が挟まるかで行う（実測）:

```
$ go mod why -m github.com/go-sql-driver/mysql
github.com/rikukaInoue/greenfield/core/authz/localauthz
github.com/rikukaInoue/greenfield/core/authz/localauthz.test   ← テスト専用の印
github.com/go-sql-driver/mysql

$ go mod why -m github.com/danielgtaylor/huma/v2
github.com/rikukaInoue/greenfield/core/httpapi                 ← .test を挟まない = 本体の依存
github.com/danielgtaylor/huma/v2
```

現時点の direct require の内訳:

| モジュール | 由来 | 区分 |
|---|---|---|
| `huma/v2` | httpapi, problem | 本 ADR |
| `chi/v5` | httpapi | 本 ADR / ADR 0001 |
| `open-feature/go-sdk` | flags | ADR 0010 |
| `go-sql-driver/mysql` | localauthz_test のみ | テスト専用（ADR 0004） |

## 還流

プロダクション版の `internal-01` にも「core のツリー図」と実物が食い違う同じ問題が
起きうる。持ち帰るのは判断そのものではなく**運用**: (1) core に direct require を
足す PR には対応する ADR 番号を書く、(2) テスト専用依存の条件（`_test.go` のみ /
`go mod why -m` で判別）、(3) ツリー図は「到達点」ではなく実物に合わせて更新し、
未実装のエントリには注記を付ける（#138 の形式）。
