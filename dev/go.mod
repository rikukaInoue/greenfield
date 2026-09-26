module github.com/rikukaInoue/greenfield/dev

go 1.26

require (
	github.com/rikukaInoue/greenfield/core v0.0.0-00010101000000-000000000000
	github.com/rikukaInoue/greenfield/services/photo v0.0.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/danielgtaylor/huma/v2 v2.39.1 // indirect
	github.com/go-chi/chi/v5 v5.3.1 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
)

replace github.com/rikukaInoue/greenfield/core => ../core

replace github.com/rikukaInoue/greenfield/services/photo => ../services/photo
