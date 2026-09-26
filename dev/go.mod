module github.com/rikukaInoue/greenfield/dev

go 1.26

require (
	github.com/rikukaInoue/greenfield/core v0.0.0-00010101000000-000000000000
	github.com/rikukaInoue/greenfield/services/photo v0.0.0
)

require (
	github.com/danielgtaylor/huma/v2 v2.39.1 // indirect
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

replace github.com/rikukaInoue/greenfield/core => ../core

replace github.com/rikukaInoue/greenfield/services/photo => ../services/photo
