module github.com/rikukaInoue/greenfield/platform/admin

go 1.26.1

require (
	github.com/go-chi/chi/v5 v5.3.2
	github.com/rikukaInoue/greenfield/core v0.0.0
	github.com/rikukaInoue/greenfield/services/photo-client v0.0.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
)

replace github.com/rikukaInoue/greenfield/core => ../../core

replace github.com/rikukaInoue/greenfield/services/photo-client => ../../services/photo-client
