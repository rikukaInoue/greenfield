# 検証ログ

<!-- 区分: 検証ビルド / 追記専用。各エントリは 04-milestones.md のチェック # または 05-roadmap.md のステージ # に対応する -->

チェックの証跡（実験手順・結果・気づき）を時系列で追記する。規約への修正が必要な発見は「還流」節に集める。

---

## 2026-09-26 — ステージ 0.1 モジュール骨格（#28） / チェック #3（#3）

### 作ったもの

- `go.work` + マルチモジュール: `core` / `services/photo` / `services/photo-client` / `dev`（allinone + scaffold）
- `core/authz`: 差し込み口の interface 一式（Authorizer / Lister / RelationWriter / Authenticator / AssuranceChecker）、Principal と ctx 伝播、タプル表記ヘルパー（`UserRef` / `ServiceRef` / `ObjectRef`）
- `dev/scaffold`: `go run ./dev/scaffold new-service <name>` でサービス + client モジュールを生成し、`go.work` の use、`dev/go.mod` の require/replace、`dev/allinone/services.go` の登録行（ポートは既存最大 +10）を更新する。**photo 自身を scaffold の出力として生成した**（テンプレートと実物の乖離を作らない）
- `services/photo`: 3リスナー（:8080/:8081/:8082、`/healthz` のみ。huma は 0.4）。`app.Run` を `cmd/photo` と `dev/allinone` が共用
- `mise.toml` tasks: `build:ws`（ワークスペース）と `build`/`vet`/`test`/`check`（各モジュール `GOWORK=off` 単体）

### 実験: #3 モジュール境界は越境importを不能にする

手順: scaffold で `gear` を一時生成し、`services/photo/usecase` に `import _ ".../services/gear/domain"` を置いて2モードでビルド。

| モード | 結果 |
|---|---|
| ワークスペース（`go build` in go.work） | **ビルド成功**（越境が通る） |
| `GOWORK=off`（`services/photo` で単体ビルド） | **失敗**: `no required module provides package .../services/gear/domain` |
| `GOWORK=off` + `photo/go.mod` に `gear-client` の require+replace | 成功（client モジュールのみ可） |

期待どおり「ビルド不能（clientモジュールのみ可）」が成立する。ただし条件付き。

### 気づき

1. **ワークスペースモードは境界を守らない。** `go.work` の `use` に載ったモジュールは、`go.mod` に require がなくても import が解決する。したがって規約 internal-01 の「CIは各モジュールを単体でビルドしてgo.modの自己完結性を毎回検証」は _必須_ であり、`GOWORK=off` で回す（`mise run build`）。日常のワークスペースビルドが通っても越境している可能性があるため、0.6 の CI は必ず `GOWORK=off` を使う。
2. **モジュール間の結線は `replace`。** タグを打たない規約のもとで `GOWORK=off` ビルドを成立させる手段は `require v0.0.0` + `replace => ../../core` の組。逆に言えば、他サービスへの依存は go.mod に replace 行として現れるので、PR レビューで境界違反（`-client` 以外への replace）が目視できる。
3. **`handler/internal/` は Go の internal パッケージ規則と衝突する。** `internal` ディレクトリは親（`handler/`）配下からしか import できず、`app/`（や `cmd/`）から配線できない。`handler/internalapi/` に改名した。→ 還流
4. `app/` パッケージを規約のツリーに追加した。`cmd/<name>/main.go` と `dev/allinone` が同じ起動手順（`app.Run`）を共用するため。→ 還流（ツリー図への追記）
5. 規約 internal-01 は末尾で「単一Postgresクラスタ」と書くが、検証版は 02 のとおり MySQL 8。規約側の記述ゆれ。→ 還流

### 還流

- [ ] internal-01: `handler/internal/` → Go の internal 規則に抵触。`internalapi/` 等へ改名を提案
- [ ] internal-01: ツリー図に `app/`（組み立て・起動。cmd と allinone が共用）を追加
- [ ] internal-01: 「CIは各モジュールを単体でビルド」に `GOWORK=off` を明記（ワークスペースは境界を守らないことを理由として添える）
- [ ] internal-01 末尾の Postgres 記述（MySQL との整合）

---

## 2026-09-26 — ステージ 0.2 DB基盤（#29） / チェック #1（#1）

### 作ったもの

- `deploy/compose/compose.yaml`: mysql:8.4（utf8mb4）。`mise run db:up` でヘルシーになるまで待つ
- `deploy/compose/mysql/init/01-databases.sql`: database（photo / platform）と、サービスごとの2ユーザー `<name>_app`（自DBのDMLのみ）/ `<name>_migrate`（自DBのDDL+DML）。**他サービスのdatabaseには SELECT も付与しない**。DBレベル操作はここ（platform運用相当）が持ち、マイグレーションには入れない
- `services/photo/migrations/`: expand / contract 二系統を `go:embed`。`photo migrate expand|contract|status` サブコマンド。履歴テーブルは `photo_migrations_expand` / `photo_migrations_contract` に分離。接続に `lock_wait_timeout=5` / `innodb_lock_wait_timeout=5` を付与し、ロック待ちタイムアウト（1205/1206/3572）なら3回までリトライ。アプリ起動時の自動適用はしない
- 初期 expand: `photos`（owner_subject / caption / visibility / gear_item_id / timestamps）
- scaffold: テンプレートに migrations 一式と migrate サブコマンドを追加。生成時に `go mod tidy` と init SQL への database/ユーザー追記を行う（gear で dry run 済み）
- テスト: `migrations_test.go`（DB到達時のみ。セッション変数が効いていること、履歴テーブルが系統別に存在すること）

### 実験: #1 GRANT分離は誤クエリを実行時に落とす

手順: root で `gear.items` を一時作成し、`photo_app` / `photo_migrate` で越境 SELECT。

| 操作 | 結果 |
|---|---|
| `photo_app`: `SELECT * FROM gear.items` | **ERROR 1142 SELECT command denied** |
| `photo_app`: `SELECT COUNT(*) FROM photo.photos` | 成功（0件） |
| `photo_app`: `CREATE TABLE photo.t` | ERROR 1142 CREATE command denied（app と migrate の分離） |
| `photo_migrate`: `SELECT * FROM gear.items` | ERROR 1142 SELECT command denied |
| `photo_app`: `SHOW DATABASES` | information_schema / performance_schema / photo のみ（gear は存在自体が見えない） |

期待どおり「権限エラー」。マイグレーションの適用は `expand version=1`、再実行は `no change`、`status` は系統別に表示。

### 気づき

1. golang-migrate v4.20.1 は `go-sql-driver/mysql` の v1.5.0 を最小要求に持つ。`go get @latest` で v1.10.1 に上げる必要がある（tidy だけでは古いまま）。
2. `go:embed` は `_` / `.` 始まりのパスを既定で除外する。`contract/` が空（`.gitkeep` のみ）の間は `all:` プレフィックスが要る。scaffold テンプレートの `__name__` ディレクトリも同じ理由で `all:templates`。
3. golang-migrate の `iofs` は `NNNNNN_name.up.sql` 以外のファイル（`.gitkeep` / `README.md`）を無視するため、ディレクトリを丸ごと embed してよい。
4. 履歴テーブルは `migratemysql.Config{MigrationsTable}` で系統別に切れる。DSN の `x-migrations-table` より、`openDB` でセッション変数を足す経路に統一した方が見通しがよい。
5. scaffold が生成モジュールで `GOWORK=off go mod tidy` を走らせるため、テンプレートに go.sum を持たなくて済む（依存はモジュールキャッシュから解決）。

### 還流

- [ ] internal-05: 「アプリ実行ユーザー（DMLのみ）」と「マイグレーションユーザー（自DB内DDL）」の2ユーザー構成を明文化（現状は「サービスのマイグレーションユーザーには自database内のDDLのみを許可」の一文のみ）
- [ ] internal-05: ロック待ちタイムアウトの具体値（5秒）と MySQL エラー番号（1205 / 1206 / 3572）でのリトライ判定

### 追記: タスクランナーを Makefile → mise tasks に変更

`mise.toml` で Go のバージョンを固定している以上、タスクも同じファイルに置けば追加ツールなしで揃う。`depends` による依存（`migrate` → `db:up`）と並列実行（`check`）、`mise run` の一覧が Makefile より扱いやすい。入力→生成物のキャッシュ（sqlc / client 生成）が欲しくなった時点で Task（Taskfile）への移行を再検討する。

---

## 2026-09-26 — ステージ 0.3 sqlc + 整合CI（#30） / チェック #2（#2）

### 作ったもの

- `services/photo/sqlc.yaml`: 入力は `db/schema.sql`（自DBのスキーマ）と `db/queries/*.sql` のみ。生成先は **`repository/internal/sqlcgen`** — Go の internal 規則により `repository/` の外（usecase / handler）から import できず、「sqlc生成型はRepository実装の内部に閉じる」規約を構造で強制する
- `db/schema.sql` は手書きしない。`dev/scripts/schema-dump.sh <svc> --write|--check` が空DB（`<svc>_schemacheck`）へ expand + contract を全適用し `mysqldump --no-data` を正規化して出力／比較する。`mise run schema:dump` / `schema:check`
- `dev/querylint`: クエリのテーブル位置（FROM/JOIN/INTO/UPDATE の直後）の名前を検査し、`db.table` 形式の修飾参照と、schema.sql にないテーブルを弾く（許可リスト = 自DBの CREATE TABLE。手書きリストは持たない）。`mise run lint:queries`。単体テスト付き
- `mise run sqlc`（generate）/ `sqlc:check`（`sqlc diff`）。`check` に lint:queries と sqlc:check を追加
- 初期クエリ: CreatePhoto / GetPhoto / ListPhotosByIDs（`sqlc.slice`、ListAccessible + WHERE IN 用）/ ListPhotosByOwner
- sqlc 1.31.1 を mise.toml で固定

### 実験: #2 sqlc入力限定は越境クエリを生成時に落とす

| 手順 | 結果 |
|---|---|
| `JOIN gear.items g` を含むクエリで `sqlc generate` | **失敗**: `schema "gear" does not exist` |
| 同クエリで `querylint` | 失敗: `qualified table "gear.items" (cross-database access is forbidden)` |
| 修飾なしの未知テーブル `FROM items` | sqlc: `relation "items" does not exist` / querylint: `table "items" is not in schema.sql` |
| `usecase` から `repository/internal/sqlcgen` を import | **ビルド不能**: `use of internal package ... not allowed` |
| 正常なクエリ | sqlc generate 成功、querylint ok、`schema:check` up to date |

期待どおり「生成失敗」。二重のゲート（sqlc 自身 + 生成器非依存の lint）で成立する。

### 気づき

1. golang-migrate は系統にファイルが1つもない（contract キューが空）と `first .: file does not exist` を返す。`fs.ErrNotExist` を「適用対象なし」として扱うよう `Up` を修正（テンプレートにも反映）。
2. querylint の初版は `x.y` 形式を全て越境扱いにしていたため `p.id`（別名.カラム）を誤検出した。テーブル位置に限定して解消。正規表現ベースなので、サブクエリ等の複雑な形は sqlc 側のゲートに任せる（lint は第2のゲート）。
3. `mysqldump --no-data --compact` でも `/*!40101 ... */` 行が残るため grep で除去。`AUTO_INCREMENT=N` も除去して正規化。
4. schema dump は running mysql 上の scratch database で行うため、CI は mysql コンテナだけあればよい（mysqldump はコンテナ内のものを使う）。

### 還流

- [ ] internal-02: 「sqlc生成型はRepository実装の内部に閉じる」の実現手段として `repository/internal/` 配下への生成を推奨（コンパイラで強制できる）
- [ ] internal-05: sqlc 入力スキーマは scratch database + `mysqldump --no-data` の正規化出力とし、`/*!` 行と `AUTO_INCREMENT=` を除去する旨

---

## 2026-09-26 — ステージ 0.4 API骨格（#31） / チェック #16 前半（#16）

### 作ったもの

- `core/problem`: RFC 9457（`application/problem+json`）に機械可読の `code` を必ず持たせる型。`Install()` で `huma.NewError` を差し替え、**huma が自動生成するエラー（422 の入力検証等）にも code が載る**。汎用コード（`validation_failed` 等）とドメインコード（`photo.not_found` 形式）を使い分ける
- `core/httpapi`: リスナー（external / admin / internal）ごとの huma API 組み立てを共通化。`OpenAPIPath` / `DocsPath` / `SchemasPath` を空にしてスペック・ドキュメントをアプリから配らない（`api/` の生成物が唯一の契約置き場）。CORS ミドルウェアを入れない（全リスナーで閉）。`/healthz` は echo に直接生やして OpenAPI に載せない
- `services/photo` の3リスナーに契約を定義（実装は Phase 1 以降、`501 photo.not_implemented`）:
  - external: `POST /photos`、`POST /photos/{id}:publish`（純粋な状態遷移は `:verb`）、`GET /photos/{id}`、`GET /photos`
  - admin: `GET /photos`（オペレータ）、`POST /accounts/{subject}:delete`（危険操作。ステップアップ検証用）
  - internal: `GET /photos/{id}`、`GET /gear-items/{gear_item_id}/photos`（N+1 回避の Batch 取得）
  - OperationID = usecase 名。external と admin で応答型を別にし、管理APIの形が外部クライアントへ漏れない形にした
- sqlc をコマンド側（`db/queries/repository` → `repository/internal/sqlcgen`）と読み側（`db/queries/readmodel` → `readmodel/internal/sqlcgen`）に分割。CQS をファイル境界に出す
- `dev/genapi`: huma の型から `api/<service>/<listener>.openapi.json` を生成（`-check` で一致検証）。サーバ起動と同じ `app.APIs()` を使うため、配線とスペックが乖離しない
- `dev/scripts/api-breaking.sh`: oasdiff の破壊的変更検出と `info.version` のメジャーを機械的に連動させる
- `mise run api` / `api:check` / `api:breaking`。`check` に `api:check` を追加。oasdiff 1.32.1 を固定

### 実験: 3ポート独立

| 経路 | 結果 |
|---|---|
| `:8080 GET /photos` | 501（登録済み） |
| `:8080 POST /accounts/x:delete`（admin のパス） | **404** |
| `:8080 GET /gear-items/1/photos`（internal のパス） | **404** |
| `:8082 POST /photos`（external のコマンド） | **405** |
| 全リスナー `/openapi.json` `/docs` | 404（アプリから配らない） |
| `Origin` を付けた要求 | `access-control-allow-*` ヘッダ 0（CORS 閉） |

エラー応答は `Content-Type: application/problem+json` で `{"title","status","detail","code"}`。入力検証エラーも `code: validation_failed` + `errors[].location` が載る。

### 実験: #16 前半（oasdiff とメジャーバージョンの連動）

| 変更 | `mise run api:breaking` |
|---|---|
| 変更なし | 破壊的変更なし（exit 0） |
| フィールド追加（非破壊） | 破壊的変更なし（exit 0） |
| 応答フィールド削除 × メジャー据え置き | **exit 1**: `response-required-property-removed` + 「メジャーバージョンが未更新（base=1, revision=1）」 |
| 同じ削除 + `Version` を 2.0.0 へ | exit 0: 「メジャーバージョンが 1 → 2 に更新済み。/v2 の並行提供を確認すること」 |

期待どおり「メジャー未更新でCI失敗」。`/v2` 並行提供の手順（#16 後半）は 2.2 で完成させる。

### 気づき

1. huma の `DefaultConfig` は `SchemaLinkTransformer` を積み、応答に `$schema` と `Link` ヘッダを付ける。`SchemasPath` を空にすると解決しないURLを広告することになるため `Transformers` / `CreateHooks` も外した。
2. **bash で日本語の直後に `$var` を置くと、多バイト文字が変数名に取り込まれて `unbound variable` になる**（`$rev_major）` → `rev_major�`）。日本語メッセージ内の変数展開は `${var}` で閉じる。既存スクリプトも同様に修正。
3. `humaecho.NewV4` が echo v4 用（`New` は v5 用）。v4 を使う。
4. huma は `Errors: []int{...}` に挙げたステータスだけをスペックへ載せる。実際に返しうるコードは明示的に列挙する必要がある。
5. `api.Huma.OpenAPI().MarshalJSON()` の出力はキー順が不定なため、`json.Unmarshal` → `Encoder(SetIndent)` で正規化してからコミットする（差分レビューのため）。

### 還流

- [ ] api-design §3.1: huma の `SchemaLinkTransformer` を切る（スペックをアプリから配らない構成では `$schema` リンクが解決しない）
- [ ] api-design §3.2: エラーの `code` を huma の `NewError` 差し替えで「自動生成のエラーにも」載せる実装パターン
- [ ] api-design §3.4: 破壊的変更 → メジャー更新の連動は、スペックの `info.version` と oasdiff の exit code の組で機械化できる（クライアントのパッケージバージョンを持たない段階でも成立する）

---

## 2026-09-26 — ステージ 0.5 開発ループ（#32）

### 作ったもの

差し込み口（`core/authz`）の**ローカル実装一式**。いずれも「緩い」ではなく「本物らしく厳しい」側に寄せた（conventions/internal-04）。

- `core/authz/devtoken`: 署名検証のない自己記述トークン（`dev.<base64(JSON)>`）。本番JWTのクレーム語彙（sub / client_id / scope / aal / auth_time）を先に固定する
- `core/authz/staticauthn`: devtoken を検証する Authenticator。**トークンがなければ 401**。`ENV=production` では起動時ガードで拒否
- `core/authz/localauthz`: 擬似ReBAC（Authorizer / Lister / RelationWriter）。**サービスのDBとは別のストア**（`localauthz` database）に置き、サービスのトランザクションに参加しない。本番の authzサービス同様、業務側のロールバックでタプルは消えない（#6 の再現条件）。driver は合成ルートが注入するため core は `database/sql` だけに依存する
- `core/authz/simpleassurance`: AAL を突き合わせるだけの AssuranceChecker。不足時は **RFC 9470 の 401 + `WWW-Authenticate: Bearer error="insufficient_user_authentication", acr_values="aal2"`**
- `core/problem`: `Headers`（`huma.HeadersError`）と `Write`（ミドルウェアからの直接書き出し）を追加
- `core/httpapi`: `Authenticator` を**全リスナー**に適用。`RequireScope` を internal に適用（`internal:photo`）
- `services/photo/app`: 合成ルート。ハンドラは `Deps`（interface）を受け取る形にし、実装 import は app/ と cmd/* に閉じた。admin の危険操作に `RequireAAL(AAL2)` を配線
- `dev/devtoken` CLI（`mise run token -- --user alice [--aal 2] [--service svc-gear --scope internal:photo]`）

### 実験: Tier 1 だけで開発が回る / 素通しの直叩きを作らない

| 経路 | 結果 |
|---|---|
| トークンなし `:8080 /photos` | **401** `WWW-Authenticate: Bearer realm="greenfield"` |
| トークンなし `:8082 /photos`（admin） | **401** |
| トークンなし **`:8081 /photos/1`（internal）** | **401** — プライベート側も素通しにしない |
| 壊れたトークン | 401 `unauthenticated` |
| ユーザートークン `:8080` | 501（ハンドラ到達 = 認証通過） |
| サービストークン（scope なし）`:8081` | **403** `スコープ internal:photo が必要` |
| サービストークン（scope あり）`:8081` | 501 |
| 人のトークンで `:8081` | **403**（scope を持たない） |
| `/healthz` | 認証外（200） |
| admin 危険操作 AAL1 | **401** `insufficient_user_authentication` + `acr_values="aal2"` |
| admin 危険操作 AAL2 | 501（RequireAAL 通過） |

`localauthz` の単体テスト（DB到達時のみ）: 所有者タプルがなければ拒否 / 他人には不可視 / platform operator は `operator from parent` で閲覧可・`owner` 限定 action は不可 / `ListAccessible` は自分の分のみ・operator は全件 / 重複 write は自然冪等 / 未知の action はエラー。

`mise run scaffold gear` → allinone で photo(:8080) と gear(:8090) が同時に起動し、雛形サービスでも**トークンなしは 401**（認証が既定で有効）。

### 気づき（重要）

1. **`:verb` 規約は Echo と両立しない。** 規約は「Echo + humaecho」と「`POST /photos/{id}:publish`（AIP-136 カスタムメソッド）」の両方を定めるが、Echo のパスパラメータ構文 `:id` と衝突し、`/photos/:id:publish` としてパラメータ名が壊れる。実測:

   | アダプタ | `POST /photos/42:publish` |
   |---|---|
   | humaecho (echo v4) | **422** `required path parameter is missing: path.id` |
   | humaecho (echo v5) | **422** 同上 |
   | humachi (chi) | **200** `{"id":42}` |
   | humago (net/http ServeMux) | **panic** `bad wildcard segment` |

   API設計の中核である `:verb` を採り、ルータを **chi** に替えた。ミドルウェアは元々 net/http 形式で書いていたため、`echo.WrapMiddleware` が不要になり配線はむしろ単純になった。→ 還流（規約の修正が必要）

2. **ローカル実装を厳しくしたことで、実装のバグをテストが捕まえた。** 初版の `Can` は `viewer` を直接タプルとしてしか探さず、FGAモデルの `define viewer: owner or operator from parent` の **owner からの導出**を落としていた。所有者が自分の写真を見られないという形で露見。導出表（`grants` / `fromParent`）としてモデルをコードに写した。AllowAll で開発していたら Phase 3.2 まで発覚しない類のバグ。

3. **zsh では変数名に `path` を使ってはいけない。** `path` は `PATH` に連動する特殊変数で、`path=/photos` と置いた瞬間に PATH が壊れて `command not found: curl` になる。検証スクリプトでの事故。

4. 擬似ReBACのストアをサービスDBと分けたことで、`localauthz` 用の database・ユーザーが増えた。init SQL（platform運用相当）に置き、テーブル定義もそこに持たせた（Phase 3.2 で OpenFGA に置き換わり消える）。

### 還流

- [ ] **api-design §3.1: Echo（humaecho）は `:verb` 規約と両立しない。** humachi（chi）を既定にするか、`:verb` を諦めるかの判断が要る。実測表は上記
- [ ] internal-01: ツリー図に `app/`（合成ルート。実装 import は app/ と cmd/* に限る）を明記。ハンドラは `Deps` で差し込み口を受ける形
- [ ] internal-04: ローカル実装（擬似ReBAC）は FGAモデルの導出規則を表としてコードに写す。「viewer は owner から導出」を落とすと、所有者が自分のリソースを見られない形でしか露見しない
- [ ] internal-04: 保証レベル不足時の応答形（RFC 9470 の `WWW-Authenticate` challenge）は core 側に持たせられる（`simpleassurance` の実装形）

---

## 2026-09-26 — ステージ 1.1 Atomic / 1.2 CQS / 1.3 認可呼び出し（#34 #35 #36） / チェック #4 #5 #6 #7

### 作ったもの

- `core/consistency`: `Atomic.Do` が ctx にトランザクションを埋め、Repository が `TxFrom` で参加する。ネスト安全。`FakeAtomic` も同梱
- `services/photo/domain`: Photo Entity（不変条件と `Publish` の状態遷移のみ）、Visibility / Caption の Value Object。単体テスト5本
- `services/photo/usecase`: `Atomic` / `Eventual` の語彙、`PhotoRepository` / `PhotoReader` interface、コマンド（`PhotoCommands`）とクエリ（`PhotoQueries`）を別の型に分離
- `services/photo/repository`: sqlc 実装。`queries(ctx)` が tx の有無で `WithTx` を切り替える
- `services/photo/readmodel`: Read Model。Entity を経由せず、トランザクションの外で実行
- ハンドラを usecase へ配線。ドメインエラー → problem+json の対応表（`toHTTP`）
- `usecase.FaultInjector`: 検証用の失敗注入（`PHOTO_FAULT=before_relations|before_commit`）

### 実験

alice が3枚、bob が1枚投稿した状態で確認した。

**認可付き一覧（#7 local版）**

| 主体 | `GET /photos` |
|---|---|
| alice | 3件（自分の分のみ） |
| bob | 1件（alice の写真は見えない） |
| op（platform operator） | 4件（`operator from parent` で導出） |

`GET /photos/1` は alice が 200、bob は **404**（403 ではなく存在を伏せる）。

**Entity の状態遷移**: `POST /photos/1:publish` → `visibility=public`。2回目は **409 `photo.already_published`**（Entity が遷移を拒否）。bob からは 404。

**#4 / #5 Atomic**（`PHOTO_FAULT=before_relations`: 写真を INSERT した直後、タプル書き込みの直前で失敗）

| | 投稿前 | 投稿後 |
|---|---|---|
| `photo.photos` | 4 | **4**（増えていない） |
| owner タプル | 4 | 4 |

写真レコードもタプルも残らない。fn 内の2操作が同一トランザクションで巻き戻った。

**#6 孤児タプル**（`PHOTO_FAULT=before_commit`: タプル書き込み後、コミット直前で失敗）

| | 結果 |
|---|---|
| `photo.photos` | 4（写真は消えた） |
| owner タプル | **5**（`photo:7` が残った） |

タプルは別ストアなのでロールバックされない（[ADR 0004](adr/0004-localauthz-separate-store.md)）。その状態で alice の一覧は **3件**のまま = 孤児タプルは一覧にも詳細にも現れず無害。

**危険操作のゲート**（ステップアップ + operator の両方）

| 主体 | `POST /accounts/bob:delete` |
|---|---|
| op（AAL1） | **401** `insufficient_user_authentication` |
| alice（AAL2） | **403** `forbidden`（AAL は足りるが operator でない） |
| op（AAL2） | 200 `deleted_photos=1` |

**鮮度指定**: `GET /photos/{id}?fresh=true` で `authz.ConsistencyHigher` が authz へ透過する。擬似ReBAC は同期書き込みなので作成直後も既定で可視。OpenFGA へ差し替える 3.2 が本番検証。

**internal リスナー**: 公開済みの写真は 200、非公開は **404**（サービス間にも出さない）。

### 気づき

1. **ローカル実装の導出表に `operator` を入れ忘れ、admin が全て 500 になった。** `grants` に `viewer`/`editor`/`owner` しか書いておらず、`platform.operate` → `operator` の解決で「未知の relation」エラーになっていた。FGA モデルを表としてコードに写すなら、**全ての type の全ての relation を網羅する**必要がある。
2. **契約を先に確定させると、実装時の改善が破壊的変更になる。** `POST /photos` を 200 → 201 にしたところ `api:breaking` が正しく検出した。初版を `1.0.0` で切ったのが誤り（[ADR 0007](adr/0007-spec-version-before-implementation.md)）。200 のまま据え置き、201 への移行は 2.2 の `/v2` 並行提供の題材にする。
3. 認可の拒否は **404 で返す**ことにした（存在を伏せる）。403 だと「その ID の写真は存在する」が漏れる。一方 operator 権限の不足は 403 のままにした（リソースの存在とは無関係なため）。
4. `sqlc.slice` を使うクエリは生成関数の引数がスライスになるため、`LIMIT` と併用しづらい。一覧の件数制限は Read Model 側で切っている。

---

## 2026-09-27 — ステージ 1.3b 画像ストレージ（#74） / チェック #28 #29

題材をカメラ情報サイトにした時点で画像本体の置き場所が抜けていた。設計書（02 のドメイン節、05 のロードマップ、04 のチェックリスト）に追加し、実装した。

### 作ったもの

- **RustFS**（S3互換、compose の `rustfs`、:9000）。LocalStack の S3 は使わない（後述）
- `dev/s3admin ensure-buckets`: バケット作成。`mise run s3:up` が呼ぶ。DB の database 作成と同じくインフラ側の操作
- `services/photo/blobstore`: S3 実装（署名付きURLの発行、HeadObject、削除）。AWS SDK は photo のモジュールに閉じた
- `usecase.ImageStore` の差し込み口。マイグレーション 000002 で `object_key` / `content_type` / `size_bytes` / `status` を `ALGORITHM=INSTANT` で追加
- 状態遷移: `POST /photos`（`pending_upload` + 署名付き PUT URL）→ クライアントが直接 PUT → `POST /photos/{id}:commit`（HeadObject で確認）→ `ready`
- `photo reclaim`: 放置された `pending_upload` をオブジェクト → 行 → タプルの順に削除
- 詳細・一覧の応答に署名付きの取得URL（期限10分）を付与

### 実験: #28 有害な不整合が表に出ない

| 手順 | 結果 |
|---|---|
| 実体をアップロードせず `:commit` | **409** `photo.object_not_found` |
| 実体をアップロードせず `:publish` | **409** `photo.upload_not_finished` |
| `pending_upload` の写真を一覧 | **0件**（表示経路に出ない） |
| 実体を PUT → `:commit` | `status=ready size_bytes=89` |
| 2回目の `:commit` | **409** `photo.not_pending` |
| `:publish` | `visibility=public` |
| 署名付きURLで画像取得 | 200、元ファイルとバイト一致 |

回収ジョブ（`mise run reclaim -- --older-than 0s`）:

| | photos | pending | タプル |
|---|---|---|---|
| 回収前 | 3 | 2 | 4 |
| 回収後 | **1** | **0** | **2** |

完走した写真は残り、放置分だけが消えた。

### 実験: #29 署名付きURLが実際に検証される

同じ流れを LocalStack S3 と RustFS の両方で測った。

| 検査 | 期待 | LocalStack | RustFS |
|---|---|---|---|
| 署名付き PUT / GET | 200 | 200 | 200 |
| **署名なし PUT** | 403 | **200** | **403** |
| **署名なし GET** | 403 | **200** | **403** |
| 改ざんした署名で GET | 4xx | 403 | 400 |
| 不正なアクセスキー | 403 | 通る | 403 `InvalidAccessKeyId` |

**LocalStack は `S3_SKIP_SIGNATURE_VALIDATION=0` を設定しても署名なしアクセスを通す。**
署名付きURLが設計の前提なので、それを検証できないローカル環境には意味がない。
RustFS は S3 プロトコルの実装そのものなので、拒否経路がローカルでそのまま働く（[ADR 0008](adr/0008-object-storage-rustfs.md)）。
イメージサイズも 357MB 対 1.76GB で軽い。LocalStack は SNS / SQS 用として残した。

### 気づき

1. **zsh は `$ID:commit` を修飾子 `:c` として解釈し `1ommit` に展開する。** `:verb` のURLを変数で組むときは
   `${ID}:commit` と書かなければならない。これを踏んだ結果 `POST /photos/1ommit` が
   `GET /photos/{id}` に一致し **405 `Allow: GET`** が返り、chi のルーティング不具合だと1時間誤認した。
   `$ID:publish` はたまたま無事（`:p` が修飾子として適用されない）だったため、切り分けが余計に難しくなった。
   同じ事故を防ぐため、`app/routing_test.go` に `:verb` 到達性の回帰テストを置いた。
   これは bash の多バイト文字の件（0.4）と同じ family の罠である。
2. **停止したつもりのプロセスが生きていると、古いバイナリに当たって延々と嘘の結果が出る。**
   `pkill` の直後に `pgrep` で確認し、必要ならポートを掴んでいる PID を落とすところまでやる。
   検証のたびに `ps -o lstart` で起動時刻を見るのが確実。
3. sqlc で SELECT の列を明示するとクエリごとに別の行型が生成され、Read Model 側の変換が4本に増える。
   全列を使うなら `SELECT *` のほうが素直（contract で列を落とせば生成コードが変わり参照側が落ちるので、安全性は変わらない）。
4. `POST /photos` の応答に `upload_url` を足すのは非破壊だったため、契約のメジャー更新は不要だった。
   応答フィールドの追加は互換、削除・型変更は非互換という規約どおり。

### 還流

- [ ] internal-03: 整合性クラスの判定に「外部ストレージへの書き込み」の例を追加する。判定の軸は
      「逆方向の不整合が無害かどうか」で、無害な側を先に書くと順序が決まる
- [ ] internal-07: ローカル環境で S3 を模擬する場合、LocalStack S3 は署名付きURLの検証に使えない。
      S3互換の実サーバ（RustFS / MinIO 等）を置く
- [ ] internal-07: `:verb` のURLをシェルで組むときの罠（zsh の `$var:x` 修飾子）を注意書きとして残す

---

## 2026-09-27 — ステージ 1.4 フラグ（#37）

### 作ったもの

- `core/flags`: 宣言（フラグ名 + 既定値）を受け取り、入口のミドルウェアで1回評価して ctx へ積む。
  ハンドラ・usecase は `flags.Bool(ctx, name)` しか使わない（[ADR 0010](adr/0010-feature-flag-evaluation.md)）
- flagd を compose へ（:8013）。定義は `deploy/compose/flagd/flags.json` を git 管理し、変更を PR に載せる
- フラグ2つを宣言:
  - `ops.photo_disable_uploads`（Ops・長期）: 署名URL発行を止めるキルスイッチ
  - `release.photo_caption_to_title`（Release）: 1.5 の改名で使う読み切替。今は定義だけ

### 実験

| 操作 | `POST /photos` | 備考 |
|---|---|---|
| フラグ OFF（既定） | 200 `pending_upload` | |
| **定義ファイルを on に書き換え** | **503** `photo.uploads_disabled` | **再デプロイなし** |
| 同時に `GET /photos` | 1件（影響なし） | フラグの範囲が1つの振る舞いに閉じている |
| off に戻す | 200 | 再デプロイなしで復帰 |
| **flagd を停止した状態** | **200** | 宣言した既定値 false へ倒れる（安全側 = 既存動作） |

切替は `docker compose exec` も再起動も不要で、ファイル書き換えから約3秒で反映された。

### 気づき

1. `go get` が go.mod の go directive を `1.26` → `1.26.0` に上げ、`go.work` の `go 1.26` と
   食い違って全モジュールのビルドが落ちた（`module ... requires go >= 1.26.0, but go.work lists go 1.26`）。
   依存を追加したら `go.work` 側も揃える必要がある。
2. GitHub Actions の `services:` は `command` を指定できないため、flagd に定義ファイルを渡せない。
   CI では flagd が空の状態で起動し、アプリは宣言した既定値へ倒れる。
   フラグ依存のテストは `flags.WithValues` で ctx に値を積む形にした（flagd を立てずに ON/OFF 両方を通せる）。
3. flagd provider は `NewProvider` が `(*Provider, error)` を返す。`openfeature.SetProviderAndWait` に
   直接渡せない。

### 還流

- [ ] internal-08: 「入口で1回評価」の実装形（サービスが名前 + 既定値を宣言し、ミドルウェアがまとめて評価）を追記
- [ ] internal-07: CI の `services:` では `command` が使えないため、定義ファイルを要する依存
      （flagd 等）は compose と同じ形で立てられない。テストは既定値側で通す設計にしておく

### 追記: AWS でのフラグ基盤（Phase 7 の方針）

`go-sdk-contrib/providers` を確認したところ、AWS AppConfig 用の OpenFeature プロバイダは**存在しない**
（AWS 系は `aws-ssm` = Parameter Store だけ。実際の一覧: aws-ssm / configcat / flagd / flagd-in-process /
flagsmith / flipt / from-env / gcp / go-feature-flag / harness / launchdarkly / multi-provider / ofrep /
optimizely / prefab / rocketflag / statsig / unleash）。

AppConfig を採る方針にしたため、プロバイダは自作する（[ADR 0011](adr/0011-flag-provider-on-aws.md)、Issue #79）。
flagd を S3 sync で動かす選択肢もあったが、AppConfig の段階デプロイ（割合 + ベイク時間 +
CloudWatch アラームでの自動ロールバック）が規約の展開手順とそのまま噛み合うためそちらを採った。

あわせて判明した flagd の制約:

- flagd の `--uri` は filepath / HTTP / gRPC / Kubernetes CR / **GCS / Azure Blob / S3** を取れる。
  AppConfig は非対応
- イメージは distroless でシェルも curl も持たないため、**コンテナ内の healthcheck を書けない**。
  当初 `/flagd-build --version` を指定していて常に unhealthy になっていた。healthcheck を外し、
  到達性はアプリ側が既定値へ倒れる形で吸収する

## 2026-09-27 — ステージ 5.2 SSR（前半: Keycloak 配線前、#56）

コードは #78 に同梱して main に入った。本 PR は起動タスク・CI・記録の補完。

### 作ったもの

- `frontend/`（pnpm workspace）
  - `apps/web`: React Router 8 のフレームワークモード（`ssr: true`）。loader / action がサーバー側で photo external API を呼ぶ
  - `packages/photo-api`: `api/photo/external.openapi.json` から openapi-typescript で生成した型（手書きしない）
  - `packages/api-core`（`./server`）: openapi-fetch のミドルウェアでトークン注入・`X-Request-Id`・
    `Idempotency-Key` を付与し、`insufficient_user_authentication` を `StepUpRequired` として投げる
- 認証は暫定で、`/login` が devtoken を発行して httpOnly Cookie のセッションへ保存する（ENV=production では無効）
- 投稿は ADR 0009 の3段（作成 → ブラウザから署名URLへ直接 PUT → commit）。画像は SSR を経由しない
- `dev/s3admin ensure-buckets` がバケットの CORS（`FRONTEND_ORIGINS`、既定 `:5173` / `:3000`）も設定する

### 実験

| 操作 | 結果 |
|---|---|
| 未ログインで `/` | 302 → `/login?returnTo=%2F` |
| ログイン → 一覧 | 200。`image_url` の署名URLで表示 |
| 作成 → 署名URLへ PUT（Origin: localhost:5173） | CORS 通過、PUT 200 |
| commit → `/photos/:id?fresh=1` | 直後の詳細で自分の書き込みが見える |
| 公開 → `/?visibility=public` | 一覧に出る |
| 存在しないID | 404 |
| HTML・クライアントバンドルに `dev.` トークン / セッション秘密 | 出ない |

### 気づき

1. RustFS は既定で CORS ヘッダを返さない（プリフライトは 200 だが `Access-Control-Allow-Origin` なし）。
   バケット CORS（`PutBucketCors`）には対応しているので、バケット作成と同じ場所で設定した
2. Cookie セッションは署名のみで暗号化していないため、トークンは Cookie 値に base64 で載る。
   JS からは読めない（httpOnly）が、Keycloak 配線時はセッションIDだけを Cookie に持たせ、
   トークンはサーバー側ストアへ移す
3. 画像の直接 PUT は `clientAction` → resource route（`/resources/photos` 等）への fetch で組んだ。
   `serverAction()` はリクエスト本文をそのまま転送するため、画像が SSR に流れてしまい使えない

### 還流

- [ ] internal-07: 署名URL直 PUT を使うなら、ローカルのオブジェクトストレージにもバケット CORS が要る
- [ ] #17 は Keycloak 配線（3.1 / 3.3）後、サーバー側セッションに移してから devtools で確認してクローズする
---

## 2026-09-27 — ステージ 1.5a〜1.5e オンラインマイグレーション（#38〜#42） / チェック #15 #19 #21 #25

`caption` → `title` の改名を、負荷をかけたまま完走させた。Phase 1 の出口。

### 1.5a 観測手段

- `dev/loadgen`: external API へ連続して読み書きを流し、2xx 以外を失敗として数える。終了時に JSON、エラー1件以上で exit 1
- `dev/columncheck`: 新旧カラムの一致を検算（全行、`<=>` で NULL 安全比較）。新カラム未作成なら「expand 前」と出して抜ける

### 1.5b expand + 二重書き（#15）

- `000003_caption_to_title.up.sql`: `ADD COLUMN title VARCHAR(1000) NULL, ALGORITHM = INSTANT`。バックフィルは同梱しない
- 書き込みクエリは `caption` と `title` の両方へ同じ値を入れる（フラグに関係なく常時実行）
- 読み側は両方のカラムを取り、どちらを表示に使うかを usecase がフラグで決める

INSTANT の境界を実測: `ADD COLUMN ... NULL` は通る。**`MODIFY COLUMN` で型を縮める変更は `ERROR 1846 ALGORITHM=INSTANT is not supported. Reason: Need to rebuild the table`** となり、expand には置けない（規約の「例外」に当たる変更の具体例）。

### 1.5c バックフィル

`photo backfill title --batch N --pause D`。主キー順に区切り、バッチごとに独立したトランザクションで確定。`title IS NULL` を条件にするので冪等で、中断しても続きから進む。

初回: `filled=45 batches=3` → 一致率 **100.00%**。

### 1.5d + #25 負荷をかけたままの改名ドリル

`dev/scripts/rename-drill.sh`（`mise run rename:drill`）が一式を自動で回す。フラグは flagd の `fractional` targeting で割合を刻み、キーは `targetingKey`（= Principal.Subject）に固定。

| 段階 | 一致率 |
|---|---|
| バックフィル直後 | 100.00%（58行） |
| フラグ 1% | 100.00%（71行） |
| フラグ 50% | 100.00%（83行） |
| フラグ 100% | 100.00%（96行） |
| OFF 巻き戻し → 100% へ戻す | — |
| 負荷終了後の最終検算 | **100.00%（324行）** |

負荷の結果: **1399リクエスト、エラー0件、全て 200**。フラグ操作は定義ファイルの書き換えのみで、**再デプロイなし**（#21）。

### 1.5e contract（#19）

2つのゲートを実測で確認した。

- **ゲート2（sqlc）**: `caption` を落とした schema に対して `sqlc generate` → `column "caption" does not exist`。クエリから参照を外すまで生成が通らない
- ゲートを満たして `contract/000001_drop_caption.up.sql`（`DROP COLUMN caption, ALGORITHM = INSTANT`）を実行。生成コードから `Caption` が消え、参照側が**コンパイルエラー**になったので修正 → これが contract の安全確認そのもの

**lock timeout 実験**: 別セッションで `photos` の行をロックしたまま contract を投入。

| | 結果 |
|---|---|
| 1回目 | **`Error 1205 Lock wait timeout exceeded`**（セッションの `lock_wait_timeout=5`。グローバルは 31536000） |
| 同時のアプリのクエリ | 59リクエスト、**エラー0**（停滞なし） |
| ロック解放後のリトライ | **成功**（8秒で完了、`caption` 消滅、dirty=false） |

### 気づき（重要）

1. **ロック待ちのリトライが実は効いていなかった。** golang-migrate はドライバのエラーを独自の型で包み `Unwrap` を通さないため、`errors.As(err, &*mysql.MySQLError)` が false になり `isLockTimeout` が常に false を返していた。メッセージ中の `Error 1205` も見るようにして解決。**「リトライを書いた」だけでは効いているか分からない**という実例。
2. **失敗すると golang-migrate は dirty を立て、そのままではリトライできない。** 2回目以降が `Dirty database version 1. Fix and force version.` で止まる。`source.Prev()` でひとつ前のバージョンを求めて `Force()` し、dirty を解消してから再試行する処理を入れた。1ファイル1文で書く前提なら失敗した文は適用されていないので安全だが、複数文を1ファイルに書くと部分適用が起こりうるので手で確認が必要。→ 還流
3. **sqlc は同じ名前付きパラメータを型の異なる2カラムへ使えない。** `caption`（NOT NULL）と `title`（NULL 許容）に `sqlc.arg(caption)` を共用しようとして `named param Caption has incompatible types: sql.NullString, string`。二重書きでは呼び出し側が同じ値を2つの引数へ渡す形にする。
4. 二重書きが効いている状態ではバックフィルの対象が 0 件になる（新規行は最初から埋まる）。ドリル中の `filled=0` は正常。
5. 読み切替は「フラグ ON でも新カラムが空なら旧カラムへ倒す」形にした。バックフィル未完了の行があってもフラグを上げられるので、展開とバックフィルの順序に依存しない。
6. `SELECT *` にしていたため、`DROP COLUMN` で生成 struct から `Caption` が消え、参照側が即コンパイルエラーになった。列を明示していても同じ結果になるが、`SELECT *` でもゲートは働く。

### 還流

- [ ] internal-05: 「適用時は lock_wait_timeout を短く設定し、失敗時はリトライする」の実装には**dirty 状態の解消**が必要。golang-migrate では失敗時に dirty が立ち、`source.Prev()` + `Force()` で戻してから再試行する。1ファイル1文で書く規約もここから導かれる（部分適用を避けるため）
- [ ] internal-05: ツールのエラーがドライバのエラーを `Unwrap` しない場合があり、エラー番号での判定が効かないことがある。リトライは実際にロックを掛けて確かめる
- [ ] internal-05: INSTANT の境界の具体例（`ADD COLUMN NULL` は可、型を縮める `MODIFY COLUMN` は不可）
- [ ] internal-08: 読み切替は「新カラムが空なら旧カラムへ倒す」形にすると、フラグの展開とバックフィルの順序に依存しなくなる

## 2026-09-27 — ステージ 5.2 ブラウザ E2E（#56）

`mise run web:e2e`、CI に組み込み。投稿の3段・公開・他人の写真の不可視・#17（トークン非露出 / CORS 閉）の9本。

E2E を入れて初めて見つかったもの:

1. **`dev/allinone` が photo を空の設定で起動していた**。ポートだけを詰めた `Config` を渡しており、
   DSN・S3・flagd が空で、DB に触れる全エンドポイントが 500 になっていた。curl での確認は `cmd/photo`
   （`ConfigFromEnv`）で起動した photo に対して行っていたため気づかなかった。scaffold の生成行も同じ形だったので併せて修正
2. **ハイドレーション前の投稿**。投稿は `clientAction` でしか動かないため、JS 読込前に押すと素のフォーム送信になり失敗する。
   CI の遅い環境で顕在化した。ハイドレーションまでボタンを無効にした
3. Playwright の `webServer` に `go run` / `pnpm start` を起動させると、テスト後も子プロセスが残り
   GitHub Actions のステップが終わらない（20分以上ハング）。CI では前段で起動し、Playwright は再利用だけにした
