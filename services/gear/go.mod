module github.com/rikukaInoue/greenfield/services/gear

go 1.26.0

require (
	github.com/go-sql-driver/mysql v1.10.1
	github.com/golang-migrate/migrate/v4 v4.20.1
	github.com/rikukaInoue/greenfield/core v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/danielgtaylor/huma/v2 v2.39.1 // indirect
	github.com/go-chi/chi/v5 v5.3.1 // indirect
)

// モジュール間の依存はタグではなくreplaceで結ぶ（リポジトリ外から消費されるまでタグは打たない）。
// 他サービスへの依存は <name>-client のみ許可。実装モジュールをここに書いてはならない。
replace github.com/rikukaInoue/greenfield/core => ../../core
