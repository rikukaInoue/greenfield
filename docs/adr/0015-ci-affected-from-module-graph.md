# 0015. CI の対象は go.mod の replace から依存グラフを計算して決める

## 背景

ロードマップ 2.1 は「paths-filter でモジュール単位の build / test / migrate 検証 / lint。
依存グラフ発火（core/** → 全部、services/photo/** → photo のみ、client 再生成 → 利用側）」とする。

paths-filter（`dorny/paths-filter` 等）はパスのパターンを手で書く。依存の向きもパターンとして手で書くことになり、
次の2つがずれ得る。

- scaffold でサービスを足したとき、フィルタの追記を忘れると新サービスの CI が一切走らない（黙って通る）
- `go.mod` に依存を足したとき、フィルタ側の依存の書き足しを忘れると、依存先の変更で利用側が検査されない

どちらも「走らなかったので落ちなかった」という形で表に出ず、監査 A-1 と同じ「0件で成功」の類になる。

## 決定

**依存グラフは各モジュールの `go.mod` の `replace`（相対パス）から計算する。手書きの対応表を持たない。**

`dev/affected` が変更ファイル（base との merge-base からの差分）を受け取り、対象を JSON で返す。

| 変更 | 対象 |
|---|---|
| モジュール内 | そのモジュール + それに（推移的に）依存するモジュール |
| `services/<x>-client/` | client + それを `replace` で使うモジュール（再生成が利用側へ波及） |
| `api/<svc>/` | そのサービス（`api:check`）+ その API を使う frontend |
| `frontend/` | frontend のみ |
| `docs/`・`*.md` | なし |
| `dev/`（全モジュールの検査ツール）・モジュール外（`mise.toml` / `.github/` / `deploy/` 等） | 全部 |

- frontend が使うサービスは `frontend/packages/<svc>-api` の有無で決める（これも手書きしない）
- `replace` 先が `go.work` に無ければ失敗する。判定の理由は必ず出力し、対象0件のときもその旨を出す
- main への push は差分を見ず全部を検査する

CI は `changes`（判定）→ `go`（モジュールごとの matrix）/ `api-breaking` / `frontend` → `check`（集約）とした。
matrix の各ジョブは `MODULES` で mise のタスクを絞るので、ローカルと CI が同じタスクを通る
（`MODULES=./services/photo mise run check`）。`MODULES` に go.work に無いモジュールを書くと失敗する。

集約ジョブ `check` は、スキップを成功、失敗・中断を失敗として扱う。ブランチ保護を入れるときは
このジョブだけを必須にすればよい（対象外でスキップされたジョブが必須チェックを塞がない）。

## 影響

- サービスの追加（scaffold）も依存の追加も、`go.work` と `go.mod` を正しく書けば CI 側の変更は要らない
- `dev/` の変更は全部を検査する。`dev` は全サービスを `replace` で参照し、schema-dump / querylint / genapi 等の
  検査ツールも持つため、「dev だけ」を検査しても意味のある保証にならない
- 判定ツール自体の誤りは全モジュールの検査漏れになる。判定ロジックは一時ディレクトリに組んだ構成で
  テストする（`dev/affected`、client 再生成の波及を含む）

## 還流

規約（`internal-01` の CI 節）の「paths-filter でモジュール単位」を「`go.mod` の依存から対象を計算する」へ置き換える。
パスのパターンを手で書く方式は、サービス追加・依存追加のたびに追記が要り、書き忘れが「走らずに通る」形で潜る。
