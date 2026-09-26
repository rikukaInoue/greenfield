# 0006. モジュール境界の検証は `GOWORK=off` でしか効かない

## 背景

`conventions/internal-01` は「モジュールグラフそのものが依存規律になる」「CIは各モジュールを単体で
ビルドして go.mod の自己完結性を毎回検証する」と定める。

実測したところ、`go.work` の `use` に載っているモジュールは、`go.mod` に require がなくても
import が解決してしまう。つまり**ワークスペースモードのビルドは境界を守らない**。

| モード | `services/photo` から `services/gear/domain` を import |
|---|---|
| ワークスペース（`go build`） | ビルド成功（越境が通る） |
| `GOWORK=off`（モジュール単体） | 失敗 `no required module provides package` |
| `GOWORK=off` + `gear-client` を require | 成功（client モジュールのみ可） |

## 決定

- 日常のビルドはワークスペース（`mise run build:ws`）
- 境界の検証を含む検査は `GOWORK=off` で各モジュール単体（`mise run build` / `vet` / `test`）
- CI は後者を使う

モジュール間の結線は `require v0.0.0` + `replace ../..`。タグを打たない規約下で `GOWORK=off` ビルドを
成立させる手段がこれしかない。副作用として、境界違反（`-client` 以外への replace）が go.mod の差分として
PR レビューに現れる。

## 影響

ローカルでビルドが通っても越境している可能性がある。`mise run check` を通すまで安心できない。

## 還流

`conventions/internal-01` の「CIは各モジュールを単体でビルド」に `GOWORK=off` を明記する。
「ワークスペースは境界を守らない」ことを理由として添える。
