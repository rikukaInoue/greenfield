package app

// samber/do のコンテナで組み立てる。プロバイダはコンテナから依存を取り出して自分を作る。
// 依存は最初に取り出されたときに作られ(遅延)、Shutdown で作られた順の逆に閉じられる。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/authzhttp"
	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/authz/localauthz"
	"github.com/rikukaInoue/greenfield/core/authz/oidcauthn"
	"github.com/rikukaInoue/greenfield/core/authz/simpleassurance"
	"github.com/rikukaInoue/greenfield/core/authz/staticauthn"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/flags"
	"github.com/rikukaInoue/greenfield/core/httpclient"
	"github.com/rikukaInoue/greenfield/services/photo/blobstore"
	"github.com/rikukaInoue/greenfield/services/photo/flagsource"
	"github.com/rikukaInoue/greenfield/services/photo/gearlink"
	"github.com/rikukaInoue/greenfield/services/photo/readmodel"
	"github.com/rikukaInoue/greenfield/services/photo/repository"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
	"github.com/samber/do/v2"
)

// authzStore は認可ストアが満たす 3 つの interface をまとめたもの。
type authzStore interface {
	authz.Authorizer
	authz.RelationWriter
	authz.Lister
}

// dbHandle は *sql.DB にコンテナが呼ぶ Shutdown を足したもの。
// do は値が Shutdown を持っていれば呼ぶが、*sql.DB は Close しか持たないので包む。
type dbHandle struct{ *sql.DB }

func (d dbHandle) Shutdown() error { return d.Close() }

// 同じ型(dbHandle)が 2 つあるので名前で区別する。
const (
	nameDBPhoto      = "db.photo"
	nameDBLocalAuthz = "db.localauthz"
)

// commonPackage は両環境で共通の部品。
func commonPackage(ctx context.Context, cfg Config) func(do.Injector) {
	return func(i do.Injector) {
		do.ProvideNamed(i, nameDBPhoto, func(do.Injector) (dbHandle, error) {
			db, err := openDB(cfg.DSN)
			if err != nil {
				return dbHandle{}, fmt.Errorf("photo db: %w", err)
			}
			return dbHandle{db}, nil
		})
		do.Provide(i, func(do.Injector) (usecase.ImageStore, error) {
			return blobstore.NewS3Store(ctx, cfg.Images)
		})
		do.Provide(i, func(do.Injector) (*flags.Evaluator, error) {
			if err := flagsource.Register(ctx, "photo", cfg.Flags); err != nil {
				// フラグ基盤に繋がらなくても起動は続ける。評価は宣言した既定値へ倒れる
				slog.Warn("フラグ基盤に接続できないので既定値で動く", "config", cfg.Flags, "err", err)
			}
			return flags.NewEvaluator("photo", flagSet), nil
		})
		do.Provide(i, func(i do.Injector) (*usecase.PhotoCommands, error) {
			db, err := do.InvokeNamed[dbHandle](i, nameDBPhoto)
			if err != nil {
				return nil, err
			}
			images, err := do.Invoke[usecase.ImageStore](i)
			if err != nil {
				return nil, err
			}
			store, err := do.Invoke[authzStore](i)
			if err != nil {
				return nil, err
			}
			gear, err := do.Invoke[usecase.GearLink](i)
			if err != nil {
				return nil, err
			}
			return usecase.NewPhotoCommands(
				consistency.NewAtomic(db.DB), repository.NewPhotoRepository(db.DB), images, store, store, outboxEventual{}, gear, usecase.EnvFaults{}), nil
		})
		do.Provide(i, func(i do.Injector) (*usecase.PhotoQueries, error) {
			db, err := do.InvokeNamed[dbHandle](i, nameDBPhoto)
			if err != nil {
				return nil, err
			}
			images, err := do.Invoke[usecase.ImageStore](i)
			if err != nil {
				return nil, err
			}
			store, err := do.Invoke[authzStore](i)
			if err != nil {
				return nil, err
			}
			return usecase.NewPhotoQueries(readmodel.NewPhotoReader(db.DB), images, store, store), nil
		})
		do.Provide(i, func(i do.Injector) (*Deps, error) {
			authn, err := do.Invoke[authz.Authenticator](i)
			if err != nil {
				return nil, err
			}
			fl, err := do.Invoke[*flags.Evaluator](i)
			if err != nil {
				return nil, err
			}
			cmds, err := do.Invoke[*usecase.PhotoCommands](i)
			if err != nil {
				return nil, err
			}
			qs, err := do.Invoke[*usecase.PhotoQueries](i)
			if err != nil {
				return nil, err
			}
			db, err := do.InvokeNamed[dbHandle](i, nameDBPhoto)
			if err != nil {
				return nil, err
			}
			return &Deps{
				Authenticator: authn,
				Assurance:     simpleassurance.New(),
				Flags:         fl,
				Commands:      cmds,
				Queries:       qs,
				DB:            db.DB,
			}, nil
		})
	}
}

// localPackage はローカル(LocalDeps)の差し替え部分。
func localPackage(cfg Config) func(do.Injector) {
	return func(i do.Injector) {
		do.Provide(i, func(do.Injector) (authz.Authenticator, error) {
			a, err := staticauthn.New()
			if err != nil {
				return nil, err
			}
			return a, nil
		})
		do.ProvideNamed(i, nameDBLocalAuthz, func(do.Injector) (dbHandle, error) {
			db, err := openDB(cfg.LocalAuthzDSN)
			if err != nil {
				return dbHandle{}, fmt.Errorf("localauthz store: %w", err)
			}
			return dbHandle{db}, nil
		})
		do.Provide(i, func(i do.Injector) (authzStore, error) {
			db, err := do.InvokeNamed[dbHandle](i, nameDBLocalAuthz)
			if err != nil {
				return nil, err
			}
			return localauthz.New(db.DB, actionRelations), nil
		})
		// dev ループでは gear も staticauthn なので、devtoken の M2M（svc-photo、
		// scope internal:gear）で紐付けコマンドを送る（gear→photo の逆向きと同じ流儀）
		do.Provide(i, func(do.Injector) (usecase.GearLink, error) {
			tok := httpclient.StaticTokenSource(devtoken.Mint(devtoken.Claims{
				Subject: "svc-photo", ClientID: "svc-photo", Service: true,
				Scopes: []string{"internal:gear"},
			}))
			g, err := gearlink.New(cfg.GearInternalURL, httpclient.Client(tok))
			if err != nil {
				return nil, fmt.Errorf("gearlink: %w", err)
			}
			return g, nil
		})
	}
}

// oidcPackage は本番相当(OIDCDeps)の差し替え部分。
func oidcPackage(ctx context.Context, cfg Config) func(do.Injector) {
	return func(i do.Injector) {
		do.Provide(i, func(do.Injector) (authz.Authenticator, error) {
			a, err := oidcauthn.New(ctx, cfg.OIDCIssuer)
			if err != nil {
				return nil, fmt.Errorf("oidcauthn: %w", err)
			}
			return a, nil
		})
		do.Provide(i, func(do.Injector) (authzStore, error) {
			if cfg.M2M.TokenURL == "" || cfg.M2M.ClientSecret == "" {
				return nil, fmt.Errorf("authz を呼ぶ M2M 資格情報が未設定（M2M_TOKEN_URL / M2M_CLIENT_SECRET）")
			}
			ts := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, cfg.M2M.Scopes...)
			return authzhttp.New(cfg.AuthzURL, httpclient.Client(ts)), nil
		})
		// gear internal は internal:gear スコープを要求する。authz 用（internal:platform）とは
		// トークンを分ける——1トークンに全スコープを盛ると、漏れたときの被害が全内部APIに広がる
		do.Provide(i, func(do.Injector) (usecase.GearLink, error) {
			ts := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, "internal:gear")
			g, err := gearlink.New(cfg.GearInternalURL, httpclient.Client(ts))
			if err != nil {
				return nil, fmt.Errorf("gearlink: %w", err)
			}
			return g, nil
		})
	}
}

// buildWithDo はコンテナを作り、Deps を取り出す。
//
// 途中で失敗しても、それまでに作られたもの(開いた DB など)は Shutdown で閉じる。
func buildWithDo(ctx context.Context, cfg Config, env func(do.Injector)) (*Deps, error) {
	injector := do.New(commonPackage(ctx, cfg), env)
	d, err := do.Invoke[*Deps](injector)
	if err != nil {
		_ = shutdownErr(injector.Shutdown())
		return nil, err
	}
	d.closers = append(d.closers, func() error { return shutdownErr(injector.Shutdown()) })
	return d, nil
}

func shutdownErr(r *do.ShutdownReport) error {
	if r == nil || r.Succeed {
		return nil
	}
	var errs []error
	for _, e := range r.Errors {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}
