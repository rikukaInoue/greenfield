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
- `Makefile`: `build-ws`（ワークスペース）と `build`/`vet`/`test`（各モジュール `GOWORK=off` 単体）

### 実験: #3 モジュール境界は越境importを不能にする

手順: scaffold で `gear` を一時生成し、`services/photo/usecase` に `import _ ".../services/gear/domain"` を置いて2モードでビルド。

| モード | 結果 |
|---|---|
| ワークスペース（`go build` in go.work） | **ビルド成功**（越境が通る） |
| `GOWORK=off`（`services/photo` で単体ビルド） | **失敗**: `no required module provides package .../services/gear/domain` |
| `GOWORK=off` + `photo/go.mod` に `gear-client` の require+replace | 成功（client モジュールのみ可） |

期待どおり「ビルド不能（clientモジュールのみ可）」が成立する。ただし条件付き。

### 気づき

1. **ワークスペースモードは境界を守らない。** `go.work` の `use` に載ったモジュールは、`go.mod` に require がなくても import が解決する。したがって規約 internal-01 の「CIは各モジュールを単体でビルドしてgo.modの自己完結性を毎回検証」は _必須_ であり、`GOWORK=off` で回す（`make build`）。日常のワークスペースビルドが通っても越境している可能性があるため、0.6 の CI は必ず `GOWORK=off` を使う。
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

- `deploy/compose/compose.yaml`: mysql:8.4（utf8mb4）。`make db-up` でヘルシーになるまで待つ
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
