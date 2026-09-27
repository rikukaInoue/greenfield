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
- `core/httpapi`: リスナー（external / admin / internal）ごとの huma API 組み立てを共通化。`OpenAPIPath` / `DocsPath` / `SchemasPath` を空にしてスペック・ドキュメントをアプリから配らない（`api/` の生成物が唯一の契約置き場）。CORS ミドルウェアを入れない（全リスナーで閉）。`/healthz` はルータに直接生やして OpenAPI に載せない（**訂正（監査）**: 当初「echo に」と書いたが、同じ Phase の [ADR 0001](adr/0001-router-chi.md) で chi に置き換えた）
- `services/photo` の3リスナーに契約を定義（実装は Phase 1 以降、`501 photo.not_implemented`）:
  - external: `POST /photos`、`POST /photos/{id}:publish`（純粋な状態遷移は `:verb`）、`GET /photos/{id}`、`GET /photos`
  - admin: `GET /photos`（オペレータ）、`POST /accounts/{subject}:delete`（危険操作。ステップアップ検証用）
  - internal: `GET /photos/{id}`、`GET /gear-items/{gear_item_id}/photos`（N+1 回避の Batch 取得）
  - OperationID は usecase 名に合わせる方針（**訂正（監査 A-7 / Issue #91）**: 実際は 9 中 7 で不一致。
    `CommitPhoto`↔`CommitUpload`、`GetPhotoDetail`↔`Detail`、`AdminDeleteAccount`↔`DeleteByOwner`、
    `ListPhotosByGearItem`↔`PublicByGearItem` 等。契約にコミット済みなので修正は oasdiff に出る変更になる）。
    external と admin で応答型を別にし、管理APIの形が外部クライアントへ漏れない形にした
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
- `services/photo/app`: 合成ルート。ハンドラは `Deps`（interface）を受け取る形にし、実装 import は app/ と cmd/* に閉じた。admin の危険操作で `RequireAAL(AAL2)` を呼ぶ（**訂正（監査）**: 「app で配線」と書いたが、`app` が注入するのは `Assurance` だけで、`RequireAAL` の呼び出しはハンドラにある。規約が「ミドルウェアのパスマッピングではなくハンドラに明示する」と定めるとおりの配置）
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

> **訂正（監査 A-3 / Issue #85）**: この実験は**一覧しか試していなかった**。
> `GET /photos/{id}`（詳細）は `status` を見ていないため、画像を上げていない写真に 200 を返し、
> 存在しないオブジェクトに署名付きURLを発行する。つまり「有害な不整合が表に出ない」は
> **詳細について偽**である。主張の範囲が測定より広かった。修正は Issue #85。

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
   > **訂正（監査 A-4 / Issue #88）**: ここで「`app/routing_test.go` に回帰テストを置いた」と書いたが、
   > **そのファイルは存在しない**（デバッグ中に削除して復元しなかった）。ルーティングを覆うテストは
   > 1本も無い状態だった。実際に置く作業は Issue #88 で行う。
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
> **訂正（監査 D-3）**: 「ENV=production では無効」と書いたが、`env.ts` のガードは
> `NODE_ENV === "production"` と `ENV ∈ {production, prod}` の**両方**を要求する。
> `NODE_ENV` が未設定（`pnpm dev` 等）なら `ENV=production` でも devtoken を発行し続け、
> `SESSION_SECRET` も既定値に落ちる。Go 側（`ENV` だけで止まる）より弱い。→ Issue #87
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

> **訂正（2026-09-27、監査 A-6 / Issue #83）**: このサブコマンドは contract の片付けで削除したが
> `mise run backfill` タスクが残っており、`cmd/photo/main.go` の `default:` に落ちて
> **サーバを起動していた**（エラーではなく黙って別のことをする）。タスクを削除した。
> バックフィルは改名ごとに条件が違うため汎用化せず、ドリルの `--backfill` 引数として
> 呼び出し側が渡す形にした。

初回: `filled=45 batches=3` → 一致率 **100.00%**。

### 1.5d + #25 負荷をかけたままの改名ドリル

`dev/scripts/rename-drill.sh`（`mise run rename:drill`）が一式を自動で回す。フラグは flagd の `fractional` targeting で割合を刻み、キーは `targetingKey`（= Principal.Subject）に固定。

> **訂正（2026-09-27、監査 A-1 / Issue #83）**: 当時のドリルはバックフィルの呼び出しを `|| true` で
> 包んでいたため、**空振りしても成功と報告する**状態だった。さらに contract 後は参照先
> （フラグ定義・`caption` カラム・`photo backfill` サブコマンド）が全て消え、実行すらできなくなっていた。
> ドリルを汎用化（`--old` / `--new` / `--flag` / `--backfill` を必須引数に）し、前提が欠けていれば
> 何も実行せずに失敗する形へ直した。**下記の数値は 2026-09-27 の1回の実行結果であり、
> 現在のドリルでそのまま再現できるものではない**（`caption` は contract 済みのため、次の改名で使う）。

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

ゲートを実測で確認した。

> **訂正（監査 A-8）**: 「2つのゲートを実測で確認した」と書いたが、**実測したのは sqlc の1つだけ**である。
> さらにそれを「ゲート2」と呼んでいるが、`migrations/contract/000001_drop_caption.up.sql` は
> sqlc をゲート1、フラグ削除をゲート2と番号付けしており、文書間で番号が逆だった。
> フラグゲート（対応する `release.` フラグが削除済みであること）には**自動チェックが存在しない**。

- **sqlc のゲート**: `caption` を落とした schema に対して `sqlc generate` → `column "caption" does not exist`。クエリから参照を外すまで生成が通らない
- ゲートを満たして `contract/000001_drop_caption.up.sql`（`DROP COLUMN caption, ALGORITHM = INSTANT`）を実行。生成コードから `Caption` が消え、参照側が**コンパイルエラー**になったので修正 → これが contract の安全確認そのもの

**lock timeout 実験**: 別セッションで `photos` の行をロックしたまま contract を投入。

| | 結果 |
|---|---|
| 1回目 | **`Error 1205 Lock wait timeout exceeded`**（セッションの `lock_wait_timeout=5`。グローバルは 31536000） |
| 同時のアプリのクエリ | 59リクエスト、**エラー0**（停滞なし） |
| ロック解放後のリトライ | **成功**（8秒で完了、`caption` 消滅、dirty=false） |

> **訂正（監査 A-2 / Issue #84）**: この「`caption` 消滅」は**手元で1回 `migrate contract` を手実行した結果**
> であり、環境の性質ではなかった。当時 `mise run migrate` も CI も `migrate expand` しか流していなかったため、
> 新しいローカル環境と CI の DB には `caption` が残り、sqlc の入力（contract 後）と食い違っていた。
> #84 で `migrate` を expand → contract の順に流す形へ直し、`schema:check-live` を追加した。

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

---

## 2026-09-27 — 監査の修正 1: ゲートが嘘をつく箇所（#83）

`docs/audit-2026-09-27.md` の A-1 / A-6 / B-1。検証の証跡がこのビルドの成果物なので最優先で直した。

### `api-breaking.sh` を fail-closed に

以前は `set -uo pipefail`（`-e` なし）で、base ref が解決できないと全スペックに
「base にスペックがない（新規API）— 検査をスキップ」を出して **exit 0** していた。
将来 ref や fetch の問題が起きたら、破壊的変更ゲートが安心できるメッセージ付きで無条件に通る状態だった。

4つの経路を実測で確認した。

| 状況 | 修正後 |
|---|---|
| 正常（3スペックを比較） | exit 0、「比較したスペック: 3 / 新規: 0 / 全体: 3」 |
| 解決できない base ref | **exit 1**「base ref ... が解決できない」 |
| `api/` にスペックが無い | **exit 1**「先に mise run api を実行する」 |
| base に `api/` が無い（比較0件） | **exit 1**「比較できたスペックが0件。ゲートが空振りしている」 |

「何件比較したか」を必ず出すようにしたので、CI ログで空振りが目視できる。

### 改名ドリルを汎用化し、前提が欠けたら実行せずに落とす

以前のドリルは (1) バックフィル呼び出しを `|| true` で包んでいた (2) 各段の `columncheck` の
終了コードを見ていなかった（`| sed` で潰れていた）ため、**一致しなくても通過**した。
さらに contract 後は参照先（フラグ定義・`caption` カラム・`photo backfill`）が全て消えて実行すらできなくなっていた。

`--old` / `--new` / `--flag` / `--backfill` を必須引数にし、前提を負荷開始前に検査する形へ直した。
バックフィルは改名ごとに条件が違うため汎用化せず、呼び出し側がコマンドを渡す。

**両方向を実測した**（一時的な列 `status_v2` とフラグを立てて検証）。

| 条件 | 結果 |
|---|---|
| 二重書きが無い（負荷中の新規行が未反映） | **exit 1**「フラグ 1% の時点で一致しなかった」（一致率 97.99%） |
| 二重書き相当のトリガを入れた | exit 0、400リクエスト・エラー0件・一致率 100.00% |
| `caption`（contract 済み）を指定 | **exit 1**「photos.caption は存在しない」（負荷を流す前に落ちる） |
| 存在しないフラグ名 | **exit 1**「フラグ ... が flags.json に無い」 |
| 引数不足 | **exit 1** |

旧版なら1つ目のケースは「成功」と報告していた。

### `mise run backfill` の削除

contract の片付けでサブコマンドを削除したのにタスクが残り、`cmd/photo/main.go` の `default:` に落ちて
**サーバを起動していた**（エラーではなく黙って別のことをする）。タスクを削除した。

### 気づき

1. **`go run` は終了コードを伝播しない。** exit 2 のプログラムを `go run` で実行すると
   シェルには **1** が返り、`exit status 2` は stderr に出るだけ。
   `columncheck` の「カラムが無い（2）」と「一致しない（1）」をドリルが区別できず、
   負荷を流した後で誤ったメッセージと共に落ちていた。ドリル内でツールを事前ビルドして解決した
   （負荷中にコンパイルが走るのを避ける意味もある）。
   **終了コードで分岐するスクリプトから `go run` を呼んではいけない。**
2. `columncheck` に終了コードの意味を持たせた（0 = 一致 / 1 = 不一致 / 2 = カラムが無い）。
   「まだ expand していない」と「一致しない」は呼び出し側にとって全く違う意味なので分ける必要がある。
3. ドリルの後片付け（`git checkout` でフラグ定義を復元）は、コミットしていない一時フラグも消す。
   検証用のフラグを足して2回連続で回すと2回目が失敗した。挙動としては正しいが、
   使う側が知っていないと混乱する。

### 還流

- [ ] **ゲートは fail-closed で書く。** 比較対象が見つからないときに「スキップ」で成功にする検査は、
      壊れたときに緑になる。CI の検査は「実際に何件検査したか」を出力し、0件なら失敗させる
- [ ] **検証スクリプトの中で失敗を飲まない。** `|| true` と「終了コードを見ないパイプ」は、
      検証そのものを無意味にする。各段で不変条件を検査し、破れたら即座に落とす
- [ ] `go run` は終了コードを 1 に丸める。終了コードで分岐する箇所では事前ビルドする
## 2026-09-27 — ステージ 5.2 SSR でのフラグ判定（#56）

### 作ったもの

- SSR も OpenFeature + flagd provider（gRPC、Go 側と同じ既定の resolver）でフラグを評価する。
  宣言（名前 + 既定値）を持ち、root の middleware で1リクエスト1回評価して context へ積む。
  ターゲティングキー・属性は `core/flags` と同じ（subject / kind）
- `ops.photo_disable_uploads` が ON の間、一覧の「投稿する」を「投稿を一時停止中」に替え、投稿画面は案内を出してフォームを無効化する。
  API 側の 503 と同じフラグ・同じキーなので、表示と API の判定が一致する

### 実験（使い捨て flagd を :18013 で起動）

| 状態 | 一覧 | 応答 |
|---|---|---|
| flagd 稼働・OFF | 投稿ボタン | 0.05s |
| 定義ファイルを on に書き換え | 投稿を一時停止中 | 0.02s（再起動なし） |
| 起動時から flagd に繋がらない | 投稿ボタン（既定値） | 0.01s |
| 稼働中に flagd を停止 | 投稿ボタン（既定値） | 0.21s（評価上限） |

### 気づき

1. **flagd provider（JS）は接続が切れると、評価のたびに再接続を待ち `deadlineMs` が効かない**（1評価 6〜7秒）。
   flagd の障害がそのまま全ページの遅延になる。provider が READY / STALE でなければ評価せず既定値、
   評価自体にも 200ms の上限を付けた。Go 側（`core/flags`）も同じ性質か要確認
2. Playwright の `webServer` は `go run` / `pnpm` 経由だと子プロセスが残って終了しない。`exec` で本体を起動し、
   `gracefulShutdown` で SIGTERM を送る形にしてローカルでも CI でも終わるようにした
3. 計測中、古い SSR プロセスがポートを掴んだまま新しいプロセスが `EADDRINUSE` で起動に失敗し、
   古いビルドを測っていた。ポートで PID を引いて止めるまで気づかなかった

---

## 2026-09-27 — フラグの評価をプロセス内へ移す（ADR 0013）

監査 C-2（プロバイダのグローバル登録とドメイン名の無効化）と B-2（CI の flagd が飾り）の修正を兼ねる。

### きっかけ

ADR 0011 で「AWS では AppConfig 用のプロバイダを自作する」と決めたが、AppConfig は
**設定ドキュメントを受け取ってプロセス内で評価する**形である。一方こちらは flagd の既定
（`rpc` = flagd 側で評価）で動いていた。ローカルと本番で評価の場所が違っていた。

「AppConfig に合わせるなら REST（OFREP）でよいのでは」という案を検討したが**誤り**だった。
OFREP もリモート評価であり、軸はプロトコルではなく**評価の場所**である。
加えて Go の OFREP プロバイダ（`providers/ofrep@v0.1.7`）は SSE を実装しておらず、
キャッシュも持たない（バルク評価エンドポイントを叩くだけ）。flagd 側は SSE を持つが
（`--ofrep-sse-enabled`、既定で有効）、Go では受け側が無い。

「定義を配ってローカルで評価する」形は業界の主流である: AppConfig / GrowthBook /
LaunchDarkly（サーバ SDK）/ Unleash はすべてこちら側で、flagd の `rpc` / OFREP の方が珍しい。

### 変えたこと

- `services/photo/flagsource` を追加。`FLAGS_SOURCE=sync`（既定、flagd の `:8015` から同期）と
  `file`（`FLAGS_FILE` を直接読む）を選べる
- **`core` には置かない**。プロバイダは grpc / connect を持ち込むため、規約の「core は軽依存に保つ」に反する
  （`blobstore` で AWS SDK をサービス側に閉じたのと同じ扱い）。`core/flags` は OpenFeature SDK にしか依存しない
- 登録を `SetNamedProviderWithContextAndWait(ctx, "photo", provider)` に変更（監査 C-2）
- compose の公開ポートを `:8013` → `:8015` へ
- **CI から flagd を外し `FLAGS_SOURCE=file` にした**。従来の CI の flagd は `services:` に
  `command` を渡せず定義を配れないため、立てても何も検査しない飾りだった（監査 B-2）
- `core/flags` に `Evaluator` のテストを追加（in-memory プロバイダを使用）
## 2026-09-27 — 監査の修正 2: スキーマの二重化（#84）

`docs/audit-2026-09-27.md` の A-2。**sqlc は contract 後のスキーマに対して生成され、
テストは expand のみの DB に対して走っていた**（生成と実行が別のスキーマを見ていた）。

### 再現

まっさらな DB に `migrate expand` だけを適用した結果と、sqlc の入力を比べた。

| | `caption` 列 |
|---|---|
| `db/schema.sql`（sqlc の入力、`schema:check` の基準） | **無い** |
| `migrate expand` だけを適用した DB（= 当時のローカルと CI） | **ある** |

`mise run migrate` も `infra:up` も CI も `migrate expand` しか流しておらず、
`photo migrate contract` はどのタスクにも CI にも現れていなかった。
`dev/scripts/schema-dump.sh` だけが expand + contract を**使い捨ての DB**に適用していたため、
`schema:check` は通り続け、乖離が見えなかった。

実行時に壊れていなかったのは、sqlc が `SELECT *` を明示的な列リストへ展開しているという偶然に依っていた。

### 直したこと

- `mise run migrate` を **expand → contract** の順で流す形にした。
  デプロイ順序の実演（Phase 2.3）のために `migrate:expand` / `migrate:contract` は個別タスクとして残す
  （本番は「expand はデプロイ前、contract は後続リリース」のまま。揃えるのはローカルと CI だけ）
- `infra:up` も同様
- **`schema:check-live`** を追加。`schema:check`（使い捨て DB との比較）では実行時の乖離を検出できない
- CI に `schema:check-live` のステップを追加

### 実測

| 検査 | 結果 |
|---|---|
| `sync`: 定義ファイルの書き換えが届く | ON へ切り替わる（503 `photo.uploads_disabled`） |
| **`sync`: flagd を停止した状態** | **ON を保持**（最後に同期した定義で評価を続ける） |
| `file`: 定義ファイルの書き換えが届く | ON へ切り替わる（ファイル監視） |
| `file`: flagd を立てずに起動 | 警告・エラーなし。評価経路が通る |
| ドメインが違うプロバイダの分離 | `svc-a` と `svc-b` で別の値を返す |

従来の `rpc` では flagd 停止時に既定値（OFF）へ倒れていた。プロセス内評価にしたことで、
**フラグ基盤が落ちても直前の状態を保つ**という AppConfig / GrowthBook と同じ性質になった。

### 気づき

1. **「プロバイダを差し替えられる」だけでは足りない。** OpenFeature の interface が同じでも、
   評価の場所（プロセス内 / リモート）が変わると**障害時の挙動が変わる**。
   ローカルで確認した縮退の挙動が本番で再現しない、という形の乖離になる
2. **プロトコルの類似は形の類似ではない。** 「AppConfig は REST だから OFREP に合わせる」は
   一見もっともらしいが、AppConfig は定義を配る側、OFREP は評価する側で、合わせるべき軸が違った
3. 古いプロセスが残っていて旧バイナリに当たり、`rpc` のまま 200 が返るのを一度 in-process の
   結果と誤認した。監査 F の指摘（`pkill` の後に `pgrep` で確認する）をその場で踏んだ

### 還流

- [ ] `internal-08` にプロバイダの選び方を書く。**評価の場所をローカルと本番で揃える**
- [ ] 無名のプロバイダ登録（`SetProvider`）を使わない。ドメイン名を渡すクライアントを作っても効かない
- [ ] CI で「立てただけで何も検査しない依存」を作らない。設定を渡せないサービスコンテナは飾りである
## 2026-09-27 — SSR のフラグ評価をやめ、判定を API から受け取る（ADR 0014）

### 作ったもの

- photo: `GET /photos` に `can_create`（必須の真偽値）。`PhotoCommands.CanCreate` が `Create` と同じ判定
  （キルスイッチ + 主体の有無）を返す。テストで両者が食い違わないことを検査
- SSR: OpenFeature・flagd provider・フラグ宣言を削除。一覧は `can_create` で投稿ボタンを出し分ける

### 気づき

1. SSR で同じフラグを評価する形（#96）は、Go 側の評価方式の変更（ADR 0013、in-process 化）に追従しないと
   SSR だけ既定値に倒れ、表示と API が黙ってずれる。評価を1箇所に閉じれば、この種のずれは構造的に起きない
2. リリース用フラグ（オンラインマイグレーションの読み切替）は API の契約を変えないので、SSR はそもそも知る必要がない。
   SSR がフラグを気にしたのはキルスイッチの「投稿できるか」だけで、欲しかったのはフラグではなく判定結果だった
3. 判定を返すときはフラグ名ではなく「できること」を契約に載せる。フラグは消えるので、名前を載せると削除が破壊的変更になる
| まっさらな DB に `mise run migrate` | `caption` が消える（contract まで流れる） |
| `photos` に列を1つ足して `schema:check-live` | **exit 1** + diff（`+ drift_probe int DEFAULT NULL`） |
| 同じ状態で `schema:check` | **exit 0**（使い捨て DB 比較なので気づけない） |

### 気づき（重要）

**mise の `depends` は並行実行される。** 最初 `migrate` を
`depends = ["migrate:expand", "migrate:contract"]` と書いたが、contract が expand より先に走り、
テーブルが無い状態で `DROP COLUMN` しようとして空振りしていた。**タスクは成功と報告する**。

```
[migrate:contract] $ go run ./services/photo/cmd/photo migrate contract
[migrate:expand]   $ go run ./services/photo/cmd/photo migrate expand
```

まっさらな DB で `caption` が残っているのを見て初めて気づいた。順序が要る場合は `run` の配列
（順に実行される）で書く。これは監査 F-1（`infra:up` が並行 compose で競合する）と同じ原因で、
**mise の `depends` に順序を期待してはいけない**という一般則。

### 還流

- [ ] `internal-05`: 「sqlc の入力はマイグレーション全適用後の dump」と「expand だけを流す」を
      同時に採ると、**sqlc が見るスキーマと実行時のスキーマがずれる**。contract キューの消化を
      CI/CD の経路として明示しないと、この乖離は検査をすり抜ける。
      検査は「使い捨て DB との一致」と「実行時の DB との一致」の2本が必要
- [ ] タスクランナーの `depends` が並行実行かどうかを確認する。順序が要る手順を `depends` で
      並べると、**空振りしたまま成功と報告する**

---

## 2026-09-27 — 監査の修正 3: import 規律を lint で強制する（#90）

`docs/audit-2026-09-27.md` の C-4。規約は「Entity および usecase の業務判断から `replicaview` を
import できないことを**lint で強制する**」「モジュール内の細則を `go-arch-lint` または `depguard` で
CI において機械的に強制する」と定めるが、**そのような lint は存在しなかった**。
`replicaview/doc.go` は「lint で禁止する」と書いていて、裏付けのない主張だった。

### 入れたもの

`golangci-lint 2.14.0` を mise で固定し、`depguard` だけを有効にした `.golangci.yml` を置いた。
`mise run lint:imports` として `check` に組み込んだ（CI は `check` を回すので自動的に入る）。

| ルール | 対象 | 禁止 |
|---|---|---|
| `no-replicaview-in-domain` | `domain/` `usecase/` | `services/<name>/replicaview` |
| `adapters-only-in-composition-root` | `usecase/` `handler/` `domain/` `readmodel/` `repository/` | `localauthz` `staticauthn` `simpleassurance` `devtoken` `blobstore` `flagsource` |
| `no-infrastructure-in-domain` | `domain/` | `database/sql` `net/http` |

2つ目は ADR 0003（差し込み口の実装を import してよいのは `app` と `cmd` だけ）を、
コメントではなく機械で守るためのもの。

### 実測（違反を作って落ちることを1件ずつ確かめた）

| 違反 | 結果 |
|---|---|
| `usecase` → `replicaview` | 落ちる |
| `domain` → `replicaview` | 落ちる |
| `readmodel` → `replicaview` | **通る**（意図どおり。後述） |
| `handler` → `blobstore` | 落ちる |
| `usecase` → `flagsource` | 落ちる |
| `usecase` → `staticauthn` | 落ちる |
| `usecase` → `localauthz` | 落ちる |
| `domain` → `database/sql` / `net/http` | 落ちる |
| `app` → 実装パッケージ（許されるべき経路） | 通る |

`readmodel` → `replicaview` を許すのは、規約が禁じるのが「Entity および usecase の**業務判断**から」で
あり、ReplicaView は表示用のデータなので **Read Model が読むのが本来の経路**だから。
設定にコメントとして残した。

### 気づき

1. **depguard の `pkg` はプレフィックス一致で glob を解釈しない。** 最初 `services/*/replicaview` と
   書いたが、`localauthz`（ワイルドカードなし）は効くのに `*` を含む3つのルールが**黙って無効**だった。
   違反を作って1件ずつ確かめるまで気づかなかった。`files` の方は glob が効くので（`**/domain/*.go`）、
   同じ設定ファイル内で片方だけ効くという紛らわしい状態になる
2. これは監査で見つけた「ゲートが嘘をつく」と同じ形である。**lint を入れただけでは効いているか
   分からない**ので、ルールごとに違反を作って落ちることを確かめた
3. サービスごとにパスを列挙する必要があるため、**scaffold が `.golangci.yml` へ追記する**ようにした。
   gear を生成して追記と違反検出まで確認した

### 還流

- [ ] `internal-01`: 「lint で強制する」と書くだけでは実装されない。**ルールごとに違反を作って
      落ちることを確かめる**（lint の設定が黙って無効になる形がある）。
      新サービスの追加でルールが増える設定は、scaffold が追記する形にする
- [ ] `internal-01`: `readmodel` から `replicaview` は許す（表示用データを読むのは Read Model の仕事）。
      規約の「Entity および usecase の業務判断から」という限定を明示的に読む

---

## 2026-09-27 — 監査の修正 4: 運用・設定（#92）

`docs/audit-2026-09-27.md` の F-1 / F-2 / F-3。

### F-1 `infra:up` の並行競合（再現できた）

`depends = ["db:up", "s3:up", "flags:up"]` の3タスクが並行して `docker compose up` を呼ぶため、
同一プロジェクトのネットワーク作成で競合する。**実際に踏んだ**:

```
[s3:up]    Network greenfield_default Creating
[db:up]    Network greenfield_default Creating
[flags:up] Network greenfield_default Creating
[s3:up]    Network greenfield_default Created
[db:up]    Network greenfield_default Error  network with name greenfield_default already exists
```

`infra:up` を1回の `docker compose up -d --wait mysql rustfs` にまとめて直列化した。
修正後は `already exists` の出現が 0 になった。

これは #84 で観測した「mise の `depends` は並行実行される」と同じ原因である。

### F-1 flagd を待たない

flagd のイメージは distroless でシェルも curl も持たないため、compose の healthcheck を書けない
（書けば常に unhealthy になる。0.5 の作業で実際にそうなっていた）。そのため `--wait` の対象にできず、
`infra:up` は flagd の起動中に戻っていた。直後にアプリを立てると定義を同期できず、
**宣言した既定値のままプロセスの生涯を過ごす**（警告1行のみ、リトライなし）。

`dev/scripts/wait-for-flagd.sh` を追加し、ホスト側から TCP で到達性を待つようにした（`--wait` 相当の回復）。

```
[infra:up] $ dev/scripts/wait-for-flagd.sh
flagd は localhost:8015 で応答している
```

### F-2 localstack が死んだ定義

起動するタスクもコードも無い（Phase 4.2 まで使わない）。compose の `profiles: ["events"]` に移し、
既定では起動しないようにした。使うときは `docker compose --profile events up -d localstack`。

### F-3 loadgen のゴミと回収

`loadgen` は画像を上げず `:commit` も呼ばないので、書き込みごとに `pending_upload` の行と
owner タプルが残る。`reclaim` の既定（`--older-than 1h --limit 100`）では1回の後片付けに
1時間待って複数回実行が必要だった。

- `mise run reclaim:all`（`--older-than 0s --limit 100000`）を追加
- `loadgen` が終了時に「`mise run reclaim:all` で回収する」と案内する（ゴミを作る側が回収方法を示す）

実測:

| | photos | pending | タプル |
|---|---|---|---|
| 負荷後 | 836 | 814 | 842 |
| 既定の `reclaim`（1時間以上前のみ） | — | — | reclaimed=100 |
| `reclaim:all` | — | — | reclaimed=714 |
| 回収後 | **22** | **0** | **28** |

### 気づき

1. **`depends` の並行実行は2箇所で実害を出した**（#84 の migrate の順序、本件のネットワーク競合）。
   タスクランナーの `depends` は「前に実行する」であって「順に実行する」ではない。
   順序や排他が要るものは1つのタスクの `run` 配列にまとめる
2. **healthcheck を書けないイメージがある。** distroless はシェルも curl も持たないため
   コンテナ内 healthcheck が成立しない。`--wait` に頼れないので、ホスト側のプローブで代替する。
   「healthcheck を書いたのに常に unhealthy」より「書かずにホストから待つ」方が正しい
3. **ゴミを作るツールに回収方法を言わせる。** `reclaim` は存在していたが手動タスクにしか現れず、
   既定値も検証向きではなかった。作る側が案内すれば、次に使う人が同じ調査をしない

### 還流

- [ ] `internal-06`（運用系TODO）: 回収ジョブ（`pending` 状態の後片付け）を定期実行の対象として
      挙げる。実装があっても呼ぶ経路が無ければ溜まり続ける
- [ ] `internal-07`: ローカル依存の起動は直列化する。タスクランナーの並行実行と
      `docker compose` の同一プロジェクト操作は競合する
- [ ] `internal-07`: distroless のイメージは healthcheck を書けない。到達性はホスト側から待つ
## 2026-09-27 — ステージ 2.0 第2サービス骨格（gear、#43）

### 作ったもの

- `mise run scaffold gear` で gear（:8090 / :8091 / :8092）と gear-client を生成。手を加えずに `mise run check`・
  `schema:check`・`sqlc:check`・`api:check`・`api:breaking` が通る状態にした
- allinone で photo と同時に起動し、gear の3リスナーが `/healthz` 200、トークンなしで 401 を返すことを確認

### scaffold の初仕事で見つかったもの（生成直後に CI が落ちる箇所）

1. **生成した Go ファイルが gofmt を通らない**。登録行を文字列で差し込むだけで、import の並びと map の位置揃えが崩れる。
   差し込み後に `go/format` で整形するようにした
2. **クエリのないサービスで `sqlc diff` / `sqlc generate` が失敗する**（`no queries contained in paths`）。
   骨格にはクエリが無いのが当然なので、「クエリなし（sqlc 対象外）」と明示して飛ばす。黙って飛ばすと
   監査 A-1 と同じ「0件で成功」になるため、必ず出力する
3. **`dev/go.mod` を整えていない**。scaffold は生成したサービスでだけ `go mod tidy` しており、require を足した
   `dev` は `GOWORK=off` のビルドで `updates to go.mod needed` になる。`dev` でも tidy する
4. **テーブルが1つも無いと `schema-dump.sh` が黙って失敗する**。mysqldump の出力が空だと `grep -v` が終了コード 1 を返し、
   `set -euo pipefail` でメッセージなしに落ちる。空を許容した。scaffold の `schema.sql` 雛形も dump の出力と揃えた
5. **マイグレーション系の mise タスクが photo を直書きしていた**（`migrate` / `infra:up` / `migrate:expand|contract|status`）。
   scaffold で増えたサービスが対象にならないので、サービス一覧をループする形にした

いずれも「サービスが1つしかない」「テーブルとクエリがある」前提に依存していた。2サービス目を実際に作って初めて表に出た。

---

## 2026-09-27 — 監査の修正 5: 誤った主張の訂正（#89）

`docs/audit-2026-09-27.md` の A-2 / A-3 / A-4 / A-7 / A-8 / C-5 / D-3 / E。

このビルドの成果物は「動くコード」ではなく「検証の証跡」なので、記録の誤りは成果物の欠陥である。
**誤りを消さず、その場に訂正を併記した**（何を間違えたかも還流物であるため）。

### 入れた訂正（12箇所）

| 場所 | 誤っていた主張 | 実際 |
|---|---|---|
| 1.3b | 「`app/routing_test.go` に回帰テストを置いた」 | **置いていない**（デバッグ中に削除して復元せず）。Issue #88 |
| 1.3b #28 | 「有害な不整合が表に出ない」 | 実験は**一覧しか試していない**。詳細は `status` を見ておらず 200 を返す。Issue #85 |
| 1.5e | 「`caption` 消滅」 | **手元で1回 contract を手実行した結果**。環境の性質ではなかった。#84 で修正済み |
| 1.5e | 「2つのゲートを実測した」 | 実測は sqlc の1つだけ。しかも番号がマイグレーションファイルと逆。フラグゲートは自動チェックなし |
| 0.4 | 「OperationID = usecase 名」 | **9 中 7 で不一致**。契約にコミット済みなので修正は oasdiff に出る。Issue #91 |
| 0.4 | 「`/healthz` は echo に直接生やして」 | ルータは chi（同じ Phase の ADR 0001 で置換） |
| 0.5 | 「`RequireAAL(AAL2)` を app で配線」 | `app` は `Assurance` を注入するだけ。呼び出しはハンドラ（規約どおりの配置） |
| 5.2 | 「SSR の `/login` は ENV=production で無効」 | `NODE_ENV` と `ENV` の**両方**を要求。Go 側より弱い。Issue #87 |
| ADR 0004 | 「`core` は `database/sql` にしか依存しない」 | 実装は driver を知らないが、`core/go.mod` の direct require には入っている |
| ADR 0009 | 「`pending_upload` は表示経路に出さない」 | **詳細は絞っていない**。Issue #85 |
| ADR 0009 | 「再実行で回収できる」 | **タプル削除の失敗後は回収できない**（候補が `photos` 由来のため到達しない） |
| README | `mise run check` の説明 | `lint:queries` / `lint:imports` / `sqlc:check` / `api:check` も含む |

### 気づき

1. **誤りの多くは「実験の範囲より主張が広い」形だった。** #28 は一覧しか試していないのに
   「一覧・詳細・サービス間API」と書き、1.5e は1つのゲートを実測して「2つ」と書いた。
   検証を書くときは**測った範囲をそのまま書く**必要がある。
   「たぶんこうなっているはず」を測定結果と同じ文に混ぜてはいけない
2. **1回の手動実行を環境の性質として書いてしまう形もあった**（`caption` 消滅）。
   手で流したものは「手で流した」と書き、自動経路に載せるまでは性質として主張しない
3. 実装を変えたときに、それを説明した過去の記述を追わないと嘘になる（echo → chi、
   `app` での配線 → ハンドラでの呼び出し）。ADR を書くだけでは足りず、**前の記述にも訂正が要る**

### 還流

- [ ] 検証の記録は「測った範囲」と「設計上の期待」を分けて書く。
      主張の範囲が測定より広いと、後から読んだ人が検証済みだと誤認する
- [ ] 手動実行の結果を環境の性質として書かない。自動経路（タスク / CI）に載せてから性質として主張する
## 2026-09-27 — ステージ 2.1 差分検知 CI（#44）

### 作ったもの

- `dev/affected`: 変更ファイルと `go.mod` の `replace` から、検査すべきモジュールと frontend の要否を求める（ADR 0015）
- CI を `changes` → `go`（モジュールごとの matrix）/ `api-breaking` / `frontend` → `check`（集約）に分割
- mise のタスクを `MODULES` で絞れるようにした（`dev/scripts/modules.sh`）。CI の matrix は同じタスクを流す

### 判定の確認（実リポジトリの依存で、1ファイルだけ変えた場合）

| 変更 | Go の対象 | frontend |
|---|---|---|
| `services/photo/usecase/query.go` | photo, dev | 走る（photo の API を使う） |
| `services/gear/app/app.go` | gear, dev | 走らない |
| `core/flags/flags.go` | core, photo, gear, dev | 走る |
| `docs/05-roadmap.md` | なし | 走らない |

client 再生成の波及（gear が photo-client を使う構成）は、まだ実物に利用関係が無いため `dev/affected` のテストで確認した。

### 気づき

1. `modfile.ParseLax` は依存先の go.mod を読むためのもので、`replace` を読み飛ばす。これを使うと依存グラフが空になり、
   core を変えても core しか検査しない。テストで気づいた（実物の構成で試すまで分からない種類の誤り）
2. 対象を絞れるようにすると「絞った結果0件」が正当な場合と、打ち間違いで0件になる場合が混ざる。
   `MODULES` は go.work に無いものを指定したら失敗させ、対象サービスが無い検査は「対象外」と出力して飛ばす

### 実験: 実際の PR で CI がどう動くか（#22 / #23）

マージしない下書き PR を3本立て、GitHub Actions 上で走ったジョブを確認した（検証後に閉じた）。

| PR | 変更 | 走ったジョブ | 所要 |
|---|---|---|---|
| #106 | `services/photo/usecase/query.go` | changes, go(photo), go(dev), api-breaking, frontend, check | 321s |
| #107 | `core/flags/flags.go` | changes, go(core), go(photo), go(gear), go(dev), api-breaking, frontend, check | 311s |
| #108 | `docs/05-roadmap.md` | changes, check（他はスキップ） | 25s |

- **#22**: photo のみの PR で gear のジョブは作られない（matrix に入らない）
- **#23**: core の変更は core に依存する photo / gear / dev へ広がる。client は core に依存しないので走らない。
  client 再生成の波及は、実物に利用関係がまだ無いため `dev/affected` のテストで確認している
- 比較: main への push（全部）は 282s・9 ジョブ。matrix が並列なので所要時間は大きく変わらず、
  差が出るのはジョブ数（Actions の消費時間）とドキュメントのみの PR
