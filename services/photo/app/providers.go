package app

// DI の部品(プロバイダ)。wire がこれらを型で繋いで組み立てコードを生成する。
// 環境で変わるのは provideStaticAuthn/provideLocal* と provideOIDC* の組だけで、
// 残りは両環境で共通(commonSet)。

import (
	"context"
	"database/sql"
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
)

// authzStore は認可ストアが満たす 3 つの interface をまとめたもの。
// usecase は同じストアを Authorizer / RelationWriter / Lister として受け取る。
type authzStore interface {
	authz.Authorizer
	authz.RelationWriter
	authz.Lister
}

// *sql.DB が 2 つ(業務 DB と localauthz の DB)あるので、型で区別する。
// DI ライブラリは型で依存を解決するため、同じ型が 2 つあると見分けられない。
type photoDB struct{ *sql.DB }
type localAuthzDB struct{ *sql.DB }

// --- 両環境で共通 ---

func providePhotoDB(cfg Config) (photoDB, func(), error) {
	db, err := openDB(cfg.DSN)
	if err != nil {
		return photoDB{}, nil, fmt.Errorf("photo db: %w", err)
	}
	return photoDB{db}, func() { _ = db.Close() }, nil
}

func provideImages(ctx context.Context, cfg Config) (usecase.ImageStore, error) {
	return blobstore.NewS3Store(ctx, cfg.Images)
}

func provideFlags(ctx context.Context, cfg Config) *flags.Evaluator {
	if err := flagsource.Register(ctx, "photo", cfg.Flags); err != nil {
		// フラグ基盤に繋がらなくても起動は続ける。評価は宣言した既定値へ倒れる
		slog.Warn("フラグ基盤に接続できないので既定値で動く", "config", cfg.Flags, "err", err)
	}
	return flags.NewEvaluator("photo", flagSet)
}

func provideCommands(db photoDB, images usecase.ImageStore, store authzStore, gear usecase.GearLink) *usecase.PhotoCommands {
	return usecase.NewPhotoCommands(
		consistency.NewAtomic(db.DB), repository.NewPhotoRepository(db.DB), images, store, store, outboxEventual{}, gear, usecase.EnvFaults{})
}

func provideQueries(db photoDB, images usecase.ImageStore, store authzStore) *usecase.PhotoQueries {
	return usecase.NewPhotoQueries(readmodel.NewPhotoReader(db.DB), images, store, store)
}

func provideDeps(authn authz.Authenticator, fl *flags.Evaluator, c *usecase.PhotoCommands, q *usecase.PhotoQueries, db photoDB) *Deps {
	return &Deps{
		Authenticator: authn,
		Assurance:     simpleassurance.New(),
		Flags:         fl,
		Commands:      c,
		Queries:       q,
		DB:            db.DB,
	}
}

// --- ローカル(LocalDeps) ---

func provideStaticAuthn() (authz.Authenticator, error) {
	a, err := staticauthn.New()
	if err != nil {
		return nil, err
	}
	return a, nil
}

func provideLocalAuthzDB(cfg Config) (localAuthzDB, func(), error) {
	db, err := openDB(cfg.LocalAuthzDSN)
	if err != nil {
		return localAuthzDB{}, nil, fmt.Errorf("localauthz store: %w", err)
	}
	return localAuthzDB{db}, func() { _ = db.Close() }, nil
}

func provideLocalStore(db localAuthzDB) authzStore {
	return localauthz.New(db.DB, actionRelations)
}

// dev ループでは gear も staticauthn なので、devtoken の M2M（svc-photo、
// scope internal:gear）で紐付けコマンドを送る（gear→photo の逆向きと同じ流儀）
func provideLocalGear(cfg Config) (usecase.GearLink, error) {
	tok := httpclient.StaticTokenSource(devtoken.Mint(devtoken.Claims{
		Subject: "svc-photo", ClientID: "svc-photo", Service: true,
		Scopes: []string{"internal:gear"},
	}))
	g, err := gearlink.New(cfg.GearInternalURL, httpclient.Client(tok))
	if err != nil {
		return nil, fmt.Errorf("gearlink: %w", err)
	}
	return g, nil
}

// --- 本番相当(OIDCDeps) ---

func provideOIDCAuthn(ctx context.Context, cfg Config) (authz.Authenticator, error) {
	a, err := oidcauthn.New(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidcauthn: %w", err)
	}
	return a, nil
}

func provideOIDCStore(cfg Config) (authzStore, error) {
	if cfg.M2M.TokenURL == "" || cfg.M2M.ClientSecret == "" {
		return nil, fmt.Errorf("authz を呼ぶ M2M 資格情報が未設定（M2M_TOKEN_URL / M2M_CLIENT_SECRET）")
	}
	ts := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, cfg.M2M.Scopes...)
	return authzhttp.New(cfg.AuthzURL, httpclient.Client(ts)), nil
}

// gear internal は internal:gear スコープを要求する。authz 用（internal:platform）とは
// トークンを分ける——1トークンに全スコープを盛ると、漏れたときの被害が全内部APIに広がる
func provideOIDCGear(cfg Config) (usecase.GearLink, error) {
	ts := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, "internal:gear")
	g, err := gearlink.New(cfg.GearInternalURL, httpclient.Client(ts))
	if err != nil {
		return nil, fmt.Errorf("gearlink: %w", err)
	}
	return g, nil
}
