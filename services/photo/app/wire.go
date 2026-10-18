//go:build wireinject

package app

// wire への指示書。このファイルはビルドされず、`go tool wire` が読んで
// wire_gen.go を生成する。

import (
	"context"

	"github.com/goforj/wire"
)

// commonSet は両環境で共通の部品。
var commonSet = wire.NewSet(providePhotoDB, provideImages, provideFlags, provideCommands, provideQueries, provideDeps)

func initLocalDeps(ctx context.Context, cfg Config) (*Deps, func(), error) {
	wire.Build(commonSet, provideStaticAuthn, provideLocalAuthzDB, provideLocalStore, provideLocalGear)
	return nil, nil, nil
}

func initOIDCDeps(ctx context.Context, cfg Config) (*Deps, func(), error) {
	wire.Build(commonSet, provideOIDCAuthn, provideOIDCStore, provideOIDCGear)
	return nil, nil, nil
}
