module github.com/rikukaInoue/greenfield/dev

go 1.26

require (
	github.com/rikukaInoue/greenfield/core v0.0.0
	github.com/rikukaInoue/greenfield/services/photo v0.0.0
)

replace github.com/rikukaInoue/greenfield/core => ../core

replace github.com/rikukaInoue/greenfield/services/photo => ../services/photo
