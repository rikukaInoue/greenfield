package main

import (
	"context"
	photo "github.com/rikukaInoue/greenfield/services/photo/app"
	// scaffold:imports
)

// service はallinoneが起動するサービスの登録。ポート割当表の唯一の真実であり、
// scaffoldが新サービスを +10 で採番して行を追加する（conventions/internal-07）。
type service struct {
	Name string
	Base int // external。internal = Base+1, admin = Base+2
	run  func(ctx context.Context) error
}

var services = []service{
	{Name: "photo", Base: 8080, run: func(ctx context.Context) error {
		cfg := photo.ConfigFromEnv()
		cfg.ExternalAddr, cfg.InternalAddr, cfg.AdminAddr = addrs(8080)
		return photo.Run(ctx, cfg)
	}},
	// scaffold:services
}
