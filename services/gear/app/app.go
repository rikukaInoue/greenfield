// Package app は gear サービスの組み立て（合成ルート）と起動を担う。
// cmd/gear と dev/allinone の両方から同じ Run を呼ぶ（起動手順を二重に持たない）。
//
// 差し込み口（core/authz）の**実装**パッケージを import してよいのはこのパッケージと cmd/* だけである。
// usecase / handler は interface しか見ないため、本番アダプタへの差し替え（Phase 3.3）の diff は
// このファイルに閉じる（#18）。
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql" // localauthz ストアへの接続に使う driver は合成ルートが選ぶ

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/localauthz"
	"github.com/rikukaInoue/greenfield/core/authz/simpleassurance"
	"github.com/rikukaInoue/greenfield/core/authz/staticauthn"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/services/gear/handler/admin"
	"github.com/rikukaInoue/greenfield/services/gear/handler/external"
	"github.com/rikukaInoue/greenfield/services/gear/handler/internalapi"
)

// Version は OpenAPI の info.version。破壊的変更時のメジャー更新は oasdiff と連動させる
// （conventions/api-design.md §3.4）。
const Version = "1.0.0"

// InternalScope は internal リスナーが要求するスコープ。サービス間トークンに付与する。
const InternalScope = "internal:gear"

// actionRelations は action（プロダクトが使う語彙）→ FGAモデルの relation の対応。
// 本番では authzサービス内に閉じる知識であり、ここではローカル実装へ同じ表を渡す。
// action名・resource type名は後に FGA の relation へマッピングされる契約であり、増やすときは一覧を更新する。
var actionRelations = localauthz.Mapping{
	// "gear.view": "viewer",
	// "gear.edit": "editor",
}

// Config はリスナーの待ち受けアドレスと依存先。アプリが知るのはListenするポートだけであり、
// TLS・ホスト名・到達制御はインフラの持ち物（conventions/internal-07）。
type Config struct {
	ExternalAddr string
	InternalAddr string
	AdminAddr    string
	// LocalAuthzDSN は擬似ReBACのタプル置き場。サービスのDBとは別（本番の authzサービス相当）。
	LocalAuthzDSN string
}

// ConfigFromEnv は GEAR_{EXTERNAL,INTERNAL,ADMIN}_ADDR から設定を読む。
// 既定はポート割当表のとおり :8080 / :8081 / :8082。
func ConfigFromEnv() Config {
	return Config{
		ExternalAddr:  envOr("GEAR_EXTERNAL_ADDR", ":8090"),
		InternalAddr:  envOr("GEAR_INTERNAL_ADDR", ":8091"),
		AdminAddr:     envOr("GEAR_ADMIN_ADDR", ":8092"),
		LocalAuthzDSN: envOr("LOCALAUTHZ_DSN", "localauthz:localauthz@tcp(127.0.0.1:3306)/localauthz"),
	}
}

// Deps は差し込み口の実装一式。Phase 3.3 では中身だけが oidcauthn / authzhttp に変わる。
type Deps struct {
	Authenticator authz.Authenticator
	Authorizer    authz.Authorizer
	Lister        authz.Lister
	Relations     authz.RelationWriter
	Assurance     authz.AssuranceChecker

	closers []func() error
}

// Close は保持している接続を閉じる。
func (d *Deps) Close() error {
	var err error
	for _, c := range d.closers {
		err = errors.Join(err, c())
	}
	return err
}

// LocalDeps はローカル開発・CI用の実装を組み立てる（StaticAuthenticator + 擬似ReBAC）。
// 「緩い」実装ではなく「本物らしく厳しい」実装である点が重要:
// トークンがなければ401、所有者タプルがなければ不許可、一覧は ListAccessible が返したIDのみ。
func LocalDeps(cfg Config) (*Deps, error) {
	authn, err := staticauthn.New()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", cfg.LocalAuthzDSN)
	if err != nil {
		return nil, fmt.Errorf("localauthz store: %w", err)
	}
	store := localauthz.New(db, actionRelations)
	return &Deps{
		Authenticator: authn,
		Authorizer:    store,
		Lister:        store,
		Relations:     store,
		Assurance:     simpleassurance.New(),
		closers:       []func() error{db.Close},
	}, nil
}

// APIs はリスナー3系統の huma API を組み立てて返す。
// OpenAPI の生成（dev/genapi）とサーバ起動で同じ組み立てを共有するため、Run から分けてある。
// deps が nil の場合は認証ミドルウェアを付けない（スペック生成専用の経路）。
func APIs(deps *Deps) map[httpapi.Listener]httpapi.API {
	var authn authz.Authenticator
	var azr authz.Authorizer
	var lister authz.Lister
	var assurance authz.AssuranceChecker
	if deps != nil {
		authn, azr, lister, assurance = deps.Authenticator, deps.Authorizer, deps.Lister, deps.Assurance
	}
	base := httpapi.Options{Service: "gear", Version: Version, Authenticator: authn}

	ext := httpapi.New(httpapi.External, base)
	external.Register(ext, external.Deps{Authorizer: azr, Lister: lister, Assurance: assurance})

	adm := httpapi.New(httpapi.Admin, base)
	admin.Register(adm, admin.Deps{Assurance: assurance})

	// internal は「プライベートだから無認証」を採らない: 認証に加えてスコープを要求する（#27）。
	intlOpts := base
	intlOpts.RequireScope = InternalScope
	intl := httpapi.New(httpapi.Internal, intlOpts)
	internalapi.Register(intl, internalapi.Deps{Authorizer: azr, Lister: lister})

	return map[httpapi.Listener]httpapi.API{
		httpapi.External: ext,
		httpapi.Admin:    adm,
		httpapi.Internal: intl,
	}
}

// Run は3リスナーを起動し、ctxのキャンセルまたはいずれかのリスナーの失敗で全て停止する。
func Run(ctx context.Context, cfg Config) error {
	deps, err := LocalDeps(cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	return RunWith(ctx, cfg, deps)
}

// RunWith は組み立て済みの差し込み口でサーバを起動する。
func RunWith(ctx context.Context, cfg Config, deps *Deps) error {
	apis := APIs(deps)
	servers := []*http.Server{
		{Addr: cfg.ExternalAddr, Handler: apis[httpapi.External].Handler},
		{Addr: cfg.InternalAddr, Handler: apis[httpapi.Internal].Handler},
		{Addr: cfg.AdminAddr, Handler: apis[httpapi.Admin].Handler},
	}

	errc := make(chan error, len(servers))
	for _, s := range servers {
		go func() {
			if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
				return
			}
			errc <- nil
		}()
	}

	var cause error
	select {
	case <-ctx.Done():
	case cause = <-errc:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdownCtx)
	}
	return cause
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
