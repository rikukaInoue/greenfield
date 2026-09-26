module github.com/rikukaInoue/greenfield/services/photo

go 1.26

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/go-sql-driver/mysql v1.10.1
	github.com/golang-migrate/migrate/v4 v4.20.1
	github.com/rikukaInoue/greenfield/core v0.0.0-00010101000000-000000000000
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/labstack/echo/v4 v4.15.4 // indirect
	github.com/labstack/echo/v5 v5.3.0 // indirect
	github.com/labstack/gommon v0.5.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.23 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

// モジュール間の依存はタグではなくreplaceで結ぶ（リポジトリ外から消費されるまでタグは打たない）。
// 他サービスへの依存は <name>-client のみ許可。実装モジュールをここに書いてはならない。
replace github.com/rikukaInoue/greenfield/core => ../../core
