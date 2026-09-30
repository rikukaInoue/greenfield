## 2026-09-30 — 依存 CVE の検査を到達性つきで入れる（#180 / #194）

### 確かめようとしたこと

CI に脆弱性検査が1つも無い状態から最初の1本を入れる。選んだのは `govulncheck` で、
理由は**到達性解析があるので alert が0件で安定する**と期待したから。
0件で安定しない検査は PR ゲートにできない（数回で誰も見なくなる）ので、
「本当に0件で安定するのか」「affected があるとき本当に落ちるのか」の2点を実測した。

### 作ったもの

- `mise.toml` の `[tools]` に `go:golang.org/x/vuln/cmd/govulncheck = "1.8.0"`
- `[tasks.vuln]` — `GOWORK=off` でモジュールごとに `govulncheck ./...`、検査したモジュール数を出力し **0件なら落とす**
- `ci.yml` の `go` ジョブに `mise run vuln`（matrix でモジュールに絞られる＝変更のあったモジュールだけ）
- `.github/workflows/security.yml` — **毎日 05:17 JST に全モジュール**（`schedule` + `workflow_dispatch`）

### 確認（govulncheck v1.8.0 / Go 1.26.6 / DB 2026-09-28、実測）

| 確認 | 結果 |
|---|---|
| 全モジュール（9件）の検査時間 | **27秒**（並列なし、キャッシュ温） |
| `Your code is affected by` | **0件**（9モジュールすべて） |
| import にあるが呼んでいない脆弱性 | **1件** `GO-2026-6443` `google.golang.org/grpc@v1.84.0` |
| 意図的に affected を作ったとき（`golang.org/x/text@v0.3.7` の `ParseAcceptLanguage` を呼ぶ） | **exit 3** + 呼び出しトレース |
| 同・トレースの内容 | `main.go:10:46: vulncheck.main calls language.ParseAcceptLanguage` |
| 対象0件のときのガード | 落ちる（`govulncheck: 対象が0件。検査が空振りしている`） |

### 気づき

1. **到達性解析の効果が1件目から出た。** `GO-2026-6443`（grpc v1.84.0）は依存グラフに存在し、
   到達性を見ないスキャナ（Dependabot alerts、`osv-scanner`）なら鳴る。govulncheck は
   `your code doesn't appear to call these` と分類して affected に数えない。
   **そして修正版は `v1.85.0-dev.0.20260825072537-93e31b48545e` しかない。**
   到達性の判定がなければ、呼んでいない関数のために pseudo-version へ上げる判断を迫られていた

2. **exit code が 1 ではなく 3。** affected の検出は **3**、ツール自体のエラーが 1。
   `|| exit 1` で拾っているので今の形では問題ないが、
   「exit 1 だけを見る」書き方をすると affected を見逃す。`api-breaking.sh` と同じ種類の罠

3. **ワークスペースモードでは使えない。** `go.work` を有効にしたまま回すと解析が use の全モジュールに
   広がり、どのモジュールの依存として到達しているのかが分からなくなる。
   `build` / `vet` / `test` と同じく `GOWORK=off` でモジュール単位に回す必要がある（ADR 0006 と同じ理由）

4. **PR ごとの検査だけでは足りない。** 依存も toolchain も1バイトも変わらないのに、
   脆弱性データベースが増えれば明日 affected になる。**差分検知の対象にならない変化**なので、
   `security.yml` を定期実行にした。`trivy image` の日次再スキャンが要るのと同じ力学

5. **toolchain の CVE もここから出る。** `govulncheck` は Go 自体の脆弱性も報告するので、
   「依存を全部上げたのに消えない」ときは `mise.toml` の `go` を上げることになる。
   ランタイムの CVE を検知する経路が最初から付いてくるのは、他の層には無い性質
