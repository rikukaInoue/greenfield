# 境界の引き直しドリル: gear の除却と切り出しを実測する（#206）

internal-01 は「go.work 内のモジュール再編は1PRで済み、安い」と主張してきたが、
境界の3層強制で検証済みなのは**追加**（stage-20 の scaffold→gear）だけだった。
このドリルは逆方向 — gear の**除却**と**切り出し** — をブランチ上で実際にやり、
主張を実測値に置き換える。main には触れない（ドリルブランチはマージしない）。

## A. 除却ドリル: 何が、どの層で捕まるか

`git rm services/gear services/gear-client` + go.work から2行削除。
diff は **65 ファイル、−4,135 行**。そこから先に起きたことを3層に分類する。

### 第1層: 機械が止める（コンパイラ・lint・CI）

| 検出器 | 検出内容 |
|---|---|
| コンパイラ | dev（allinone/services.go:5）と photo（gearlink/gearlink.go:13）が `replacement directory ../services/gear(-client) does not exist` で即死。残存参照は**全数がエラーとして列挙される** |
| lint:replaces | 3 NG（dev の gear/gear-client、photo の gear-client）— 宙に浮いた replace を検出 |
| dev/affected | `./dev/go.mod: replace 先 ./services/gear が go.work に無い` で**ハードフェイル** |

issue の期待「CI matrix は dev/affected が対象から自動で外す」は**外れた**。
affected は黙って縮小せず、結線の矛盾を検出した時点で落ちる。除却では「自動で追従」
ではなく「矛盾が残る限り CI が通らない」という形で効く — 結果としてこちらの方が安全
（追従漏れが green のまま滑り込む余地がない）。

### 第2層: grep でしか出ない（コンパイルは通る領域）

コンパイラの射程外に **34 ファイル・約230 参照**が残る。内訳:

- CI/CD: ci.yml 23、deploy.yml 1
- compose 一式: compose.yaml 9、mysql init SQL 6、realm.json 7、Caddyfile 2、prometheus.yml 2、tier2.yaml 1
- 検証スクリプト: dev/scripts 12 本・約90 参照（pending-check 23、s2s-check 15、replica-check 15、eventual-check 12 …）
- 生成物: api/gear/*.openapi.json 3 ファイル
- ドキュメント: docs 配下 25 ファイル（過去の検証ログ含む。これは履歴なので触らない）

この層に検出器はない。除却 PR のレビューは実質この grep 棚卸しがすべてで、
**「1PR」の作業量の大半はコード削除ではなくこの層**にある。

### 第3層: 機械もgrepも判定しない（プロダクト結合）

photo の gearlink（投稿と機材の紐付け）は gear-client を import する**製品機能**であり、
gear を完全に除却して green にするには、この機能ごと消す判断が要る。
これはモジュール機構の外の話 — **境界を引き直すコストのうち、機構が安くできるのは
配線（第1層・第2層）まで。機能の結合はプロダクト判断として残り、コンパイルエラーの
形で列挙されるだけ**。ドリルとしてはこの列挙が得られた時点で成果とし、削除はしない。

## B. 切り出しドリル: replace → 公開版参照の切替コスト

gear をリポジトリ外へ移す想定で、photo の gear-client 参照を replace から
公開版（main の pseudo-version）に切り替えた。

```
go mod edit -dropreplace .../services/gear-client
go get .../services/gear-client@7a34e3a   # → v0.0.0-20260930053423-7a34e3a14ae6
GOWORK=off go build ./...                  # → 成功。コードは 1 文字も変えていない
```

- 切替コスト: **go.mod 2行 + go.sum 2行、手順 2 コマンド**。タグ不要（公開リポジトリなら
  任意コミットの pseudo-version が GOPROXY で解決できる）
- 「依存の考古学が発生しない」は成立。CIが毎回 GOWORK=off で自己完結性を検証してきた
  積み上げがここで効いた — go.mod は最初から公開消費に耐える形になっていた
- **副作用1つ**: 同一プレフィックスの require が replace なしになるため、現行の
  lint:replaces は「公開版を黙って拾う」として NG にする。本当に切り出す日には、
  切り出し済みモジュールの許可リストを replace-check.sh に足す1手順が追加で要る

## 結論: 「1PRで済み、安い」の実測形

- 1PR には収まる（65 ファイル・−4,135 行 + 棚卸し34ファイル）。ただし「安い」の内実は層で違う:
  - **第1層（コード結線）は実際に安い** — 全数がコンパイルエラー/lint で列挙され、潰せば終わり
  - **第2層（CI/compose/スクリプト）は grep 棚卸しが本体** — 検出器なし、34 ファイル
  - **第3層(製品機能の結合)は機構の外** — 安くならないし、なるべきでもない
- 切り出し方向は 4 行 + 2 コマンドで、消費側のコード変更ゼロ

internal-01 の該当文は、この3層の実測に合わせて書き換えた。

## 証跡

- ドリルブランチ: `drill/206-gear-removal`（ローカル。マージ・push しない）
- 棚卸しの生データ: 本ログの数値は `git grep -c -i gear` と各検出器の実行出力から
