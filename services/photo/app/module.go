package app

// fx のモジュール定義。プロバイダの登録と、同じ型(*sql.DB)を見分ける名前タグを置く。

import (
	"context"

	"go.uber.org/fx"
)

// commonModule は両環境で共通の部品。
var commonModule = fx.Options(
	fx.Provide(
		fx.Annotate(providePhotoDB, fx.ResultTags(`name:"photo"`)),
		provideImages,
		provideFlags,
		fx.Annotate(provideCommands, fx.ParamTags(`name:"photo"`)),
		fx.Annotate(provideQueries, fx.ParamTags(`name:"photo"`)),
		fx.Annotate(provideDeps, fx.ParamTags(``, ``, ``, ``, `name:"photo"`)),
	),
)

var localModule = fx.Module("photo-local",
	fx.Provide(
		provideStaticAuthn,
		fx.Annotate(provideLocalAuthzDB, fx.ResultTags(`name:"localauthz"`)),
		fx.Annotate(provideLocalStore, fx.ParamTags(`name:"localauthz"`)),
		provideLocalGear,
	),
)

var oidcModule = fx.Module("photo-oidc",
	fx.Provide(provideOIDCAuthn, provideOIDCStore, provideOIDCGear),
)

// buildWithFx は fx で組み立て、Deps を取り出す。
//
// fx の OnStop は Start した後でないと走らないので、ここで Start しておき、
// Deps.Close から Stop を呼ぶ(閉じる処理のエラーは Stop の戻り値で返る)。
func buildWithFx(ctx context.Context, cfg Config, env fx.Option) (*Deps, error) {
	var d *Deps
	fxApp := fx.New(
		fx.NopLogger,
		fx.Supply(cfg),
		fx.Provide(func() context.Context { return ctx }),
		commonModule,
		env,
		fx.Populate(&d),
	)
	if err := fxApp.Err(); err != nil {
		return nil, err
	}
	if err := fxApp.Start(ctx); err != nil {
		return nil, err
	}
	d.closers = append(d.closers, func() error { return fxApp.Stop(context.Background()) })
	return d, nil
}
