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
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql" // localauthz ストアへの接続に使う driver は合成ルートが選ぶ

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/authz/localauthz"
	"github.com/rikukaInoue/greenfield/core/authz/oidcauthn"
	"github.com/rikukaInoue/greenfield/core/authz/simpleassurance"
	"github.com/rikukaInoue/greenfield/core/authz/staticauthn"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/httpclient"
	"github.com/rikukaInoue/greenfield/services/gear/handler/admin"
	"github.com/rikukaInoue/greenfield/services/gear/handler/external"
	"github.com/rikukaInoue/greenfield/services/gear/handler/internalapi"
	"github.com/rikukaInoue/greenfield/services/gear/migrations"
	"github.com/rikukaInoue/greenfield/services/gear/photocatalog"
	"github.com/rikukaInoue/greenfield/services/gear/readmodel"
	"github.com/rikukaInoue/greenfield/services/gear/repository"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
	"github.com/rikukaInoue/greenfield/telemetry"
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
	// MetricsAddr は /metrics の待ち受け(例 :9092)。空なら公開しない(#59)
	MetricsAddr string
	// LocalAuthzDSN は擬似ReBACのタプル置き場。サービスのDBとは別（本番の authzサービス相当）。
	LocalAuthzDSN string
	// DSN は gear の業務データ。アプリ実行用のユーザーで接続する。
	DSN string
	// PhotoInternalURL は photo の internal リスナー。作例を M2M で引く。
	PhotoInternalURL string
	// OIDCIssuer が設定されていれば本番アダプタ（oidcauthn + 実トークン）で組む。
	OIDCIssuer string
	// M2M は photo internal を呼ぶための client_credentials（svc-gear）。
	M2M M2MConfig
}

// M2MConfig はサービス間認証のクライアント資格情報。
type M2MConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// ConfigFromEnv は GEAR_{EXTERNAL,INTERNAL,ADMIN}_ADDR から設定を読む。
// 既定はポート割当表のとおり :8080 / :8081 / :8082。
func ConfigFromEnv() Config {
	return Config{
		MetricsAddr:      os.Getenv("METRICS_ADDR"),
		ExternalAddr:     envOr("GEAR_EXTERNAL_ADDR", ":8090"),
		InternalAddr:     envOr("GEAR_INTERNAL_ADDR", ":8091"),
		AdminAddr:        envOr("GEAR_ADMIN_ADDR", ":8092"),
		LocalAuthzDSN:    envOr("LOCALAUTHZ_DSN", "localauthz:localauthz@tcp(127.0.0.1:13306)/localauthz"),
		DSN:              envOr("GEAR_DSN", "gear_app:gear_app@tcp(127.0.0.1:13306)/gear?parseTime=true"),
		PhotoInternalURL: envOr("PHOTO_INTERNAL_URL", "http://localhost:8081"),
		OIDCIssuer:       os.Getenv("OIDC_ISSUER"),
		M2M: M2MConfig{
			TokenURL:     os.Getenv("M2M_TOKEN_URL"),
			ClientID:     envOr("M2M_CLIENT_ID", "svc-gear"),
			ClientSecret: os.Getenv("M2M_CLIENT_SECRET"),
			Scopes:       []string{"internal:photo"},
		},
	}
}

// Deps は差し込み口の実装一式。Phase 3.3 では中身だけが oidcauthn / authzhttp に変わる。
type Deps struct {
	Authenticator authz.Authenticator
	Authorizer    authz.Authorizer
	Lister        authz.Lister
	Relations     authz.RelationWriter
	Assurance     authz.AssuranceChecker
	Commands      *usecase.ItemCommands
	Queries       *usecase.ItemQueries
	Links         *usecase.LinkCommands

	// DB は業務データのプール。合成ルートが昇格シグナル(プール使用率)の観測に使う(#59)
	DB *sql.DB

	closers []func() error
}

// LinksOrNil はスペック生成（deps が nil の経路）でも APIs を組めるようにする補助。
func (d *Deps) LinksOrNil() *usecase.LinkCommands {
	if d == nil {
		return nil
	}
	return d.Links
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
func LocalDeps(_ context.Context, cfg Config) (*Deps, error) {
	authn, err := staticauthn.New()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", cfg.LocalAuthzDSN)
	if err != nil {
		return nil, fmt.Errorf("localauthz store: %w", err)
	}
	db = configureDB(db)
	store := localauthz.New(db, actionRelations)
	gearDB, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("gear db: %w", err)
	}
	gearDB = configureDB(gearDB)
	// dev ループでは photo も staticauthn なので、devtoken の M2M（svc-gear、
	// scope internal:photo）で呼ぶ。Idempotency-Key と traceparent の付与は
	// 本番と同じ core/httpclient を通る
	token := httpclient.StaticTokenSource(devtoken.Mint(devtoken.Claims{
		Subject: "svc-gear", ClientID: "svc-gear", Service: true,
		Scopes: []string{"internal:photo"},
	}))
	photos, err := photocatalog.New(cfg.PhotoInternalURL, httpclient.Client(token))
	if err != nil {
		return nil, fmt.Errorf("photocatalog: %w", err)
	}
	return &Deps{
		Authenticator: authn,
		Authorizer:    store,
		Lister:        store,
		Relations:     store,
		Assurance:     simpleassurance.New(),
		Commands:      usecase.NewItemCommands(consistency.NewAtomic(gearDB), repository.NewItemRepository(gearDB)),
		Queries:       usecase.NewItemQueries(readmodel.NewItemReader(gearDB), photos),
		DB:            gearDB,
		Links:         usecase.NewLinkCommands(consistency.NewAtomic(gearDB), repository.NewLinkRepository(gearDB)),
		closers:       []func() error{db.Close, gearDB.Close},
	}, nil
}

// OIDCDeps は本番アダプタで組み立てる。認可は 4.x 時点でも localauthz のまま
// （gear の ReBAC は未使用。必要になったら photo と同様 authzhttp へ差し替える）。
func OIDCDeps(ctx context.Context, cfg Config) (*Deps, error) {
	authn, err := oidcauthn.New(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidcauthn: %w", err)
	}
	if cfg.M2M.TokenURL == "" || cfg.M2M.ClientSecret == "" {
		return nil, fmt.Errorf("photo internal を呼ぶ M2M 資格情報が未設定（M2M_TOKEN_URL / M2M_CLIENT_SECRET）")
	}
	gearDB, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("gear db: %w", err)
	}
	gearDB = configureDB(gearDB)
	ts := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, cfg.M2M.Scopes...)
	photos, err := photocatalog.New(cfg.PhotoInternalURL, httpclient.Client(ts))
	if err != nil {
		return nil, fmt.Errorf("photocatalog: %w", err)
	}
	return &Deps{
		Authenticator: authn,
		Assurance:     simpleassurance.New(),
		Commands:      usecase.NewItemCommands(consistency.NewAtomic(gearDB), repository.NewItemRepository(gearDB)),
		Queries:       usecase.NewItemQueries(readmodel.NewItemReader(gearDB), photos),
		DB:            gearDB,
		Links:         usecase.NewLinkCommands(consistency.NewAtomic(gearDB), repository.NewLinkRepository(gearDB)),
		closers:       []func() error{gearDB.Close},
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
	base := httpapi.Options{
		Service: "gear", Version: Version, Authenticator: authn,
		Revision: envOr("SERVICE_REVISION", Version), // カナリア観測用（#172）
	}

	ext := httpapi.New(httpapi.External, base) // v1 は New が作る（AddMajor は 2 以降）
	if deps != nil {
		external.Register(ext.Huma, external.Deps{Commands: deps.Commands, Queries: deps.Queries})
	} else {
		external.Register(ext.Huma, external.Deps{})
	}
	_ = azr
	_ = lister

	adm := httpapi.New(httpapi.Admin, base)
	admin.Register(adm, admin.Deps{Assurance: assurance})

	// internal は「プライベートだから無認証」を採らない: 認証に加えてスコープを要求する（#27）。
	intlOpts := base
	intlOpts.RequireScope = InternalScope
	intl := httpapi.New(httpapi.Internal, intlOpts)
	internalapi.Register(intl, internalapi.Deps{Links: deps.LinksOrNil()})

	return map[httpapi.Listener]httpapi.API{
		httpapi.External: ext,
		httpapi.Admin:    adm,
		httpapi.Internal: intl,
	}
}

// Run は3リスナーを起動し、ctxのキャンセルまたはいずれかのリスナーの失敗で全て停止する。
func Run(ctx context.Context, cfg Config) error {
	// OIDC_ISSUER の有無で配線を選ぶ（photo と同じ規約）。LocalDeps 側は staticauthn が
	// runtimeenv の許可リストで守られているので、本番で誤って選べば起動で落ちる
	build := LocalDeps
	if cfg.OIDCIssuer != "" {
		build = OIDCDeps
	}
	deps, err := build(ctx, cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	// 昇格シグナルのメトリクス(#59)。photo と同じ規約(専用ポート、未設定なら出さない)
	if cfg.MetricsAddr != "" && deps.DB != nil {
		reg := telemetry.New("gear")
		reg.ObservePool("gear", deps.DB)
		// contract キューの滞留(#200)。photo 側と同じ規約(0=滞留なし、-1=観測不能)
		reg.ObserveGauge("contract_pending", "未適用の contract マイグレーション数", func(ctx context.Context) float64 {
			n, err := migrations.PendingCount(ctx, deps.DB, migrations.Contract)
			if err != nil {
				return -1
			}
			return float64(n)
		})
		// メトリクスは排水の間も見えていてほしい(photo 側と同じ)
		metricsCtx, stopMetrics := context.WithCancel(context.WithoutCancel(ctx))
		defer stopMetrics()
		reg.Serve(metricsCtx, cfg.MetricsAddr)
	}
	return RunWith(ctx, cfg, deps)
}

// RunWith は組み立て済みの差し込み口でサーバを起動する。
func RunWith(ctx context.Context, cfg Config, deps *Deps) error {
	apis := APIs(deps)
	// 起動ログ。**これが無いと「何が何番で待ち受けているか」が記録に残らない**（#142 の実測1:
	// 稼働中のコンテナのログはサードパーティのものだけだった）。
	// 登録ルートは Debug に置く。常時出すと起動ごとに数十行増え、canonical log line の方針と
	// 釣り合わない。LOG_LEVEL=debug で見られる（Routes() はこの用途のために元からある）。
	for _, l := range httpapi.Listeners {
		api, ok := apis[l]
		if !ok {
			continue
		}
		slog.Info("listener ready", "listener", string(l), "server.address", addrOf(cfg, l))
		for _, rt := range api.Routes() {
			slog.Debug("route", "listener", string(l), "http.request.method", rt.Method, "url.path", rt.Path)
		}
	}

	// サーバ側タイムアウト(10.5)。既定の Go は全部無制限で、遅いクライアントが
	// 接続とゴルーチンを握り続けられる。ReadHeader はスローロリス対策、Write は
	// ハンドラ実行時間の上限(内部呼び出しの合計をこれ未満に収める)、Idle は
	// **ALB の idle timeout(60s)より長く**する——短いと LB が使い回そうとした接続を
	// サーバが先に閉じ、断続的な 502 になる。
	newServer := func(addr string, h http.Handler) *http.Server {
		return &http.Server{Addr: addr, Handler: h,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       65 * time.Second,
		}
	}
	servers := []*http.Server{
		newServer(cfg.ExternalAddr, apis[httpapi.External].Handler),
		newServer(cfg.InternalAddr, apis[httpapi.Internal].Handler),
		newServer(cfg.AdminAddr, apis[httpapi.Admin].Handler),
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

	// 排水(10.8): photo 側と同じ。リスナー → DB の順で閉じ、失敗は警告に出す
	start := time.Now()
	shutdownCtx, cancel := context.WithTimeout(context.Background(),
		time.Duration(envIntOr("SHUTDOWN_TIMEOUT_SECONDS", 25))*time.Second)
	defer cancel()
	clean := true
	for _, s := range servers {
		if err := s.Shutdown(shutdownCtx); err != nil {
			clean = false
			slog.Warn("排水しきれなかった(処理中のリクエストが切断された可能性)",
				"server.address", s.Addr, "error", err)
		}
	}
	if deps.DB != nil {
		_ = deps.DB.Close()
	}
	slog.Info("排水完了", "duration", time.Since(start).String(), "clean", clean)
	return cause
}

// configureDB は接続プールの上限と寿命を設定する(10.5)。
//
// 既定の Go は MaxOpenConns 無制限で、負荷時に接続が積み上がって DB 側の
// max_connections を食い潰す(プール使用率のメトリクスはあるのに設定が無い、
// という「観測が実装より先行」の状態を #215 で解消)。本番値は逆算で決める:
// MaxOpenConns × タスク数 × プロセス内の DB 接続数 < max_connections。
// ConnMaxLifetime はフェイルオーバー後の宛先切替を保証する(古い接続を持ち続けない)。
func configureDB(db *sql.DB) *sql.DB {
	db.SetMaxOpenConns(envIntOr("DB_MAX_OPEN_CONNS", 25))
	db.SetMaxIdleConns(envIntOr("DB_MAX_IDLE_CONNS", 25))
	db.SetConnMaxLifetime(time.Duration(envIntOr("DB_CONN_MAX_LIFETIME_SECONDS", 300)) * time.Second)
	return db
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// addrOf はリスナーの待ち受けアドレスを返す。起動ログ用。
func addrOf(cfg Config, l httpapi.Listener) string {
	switch l {
	case httpapi.External:
		return cfg.ExternalAddr
	case httpapi.Admin:
		return cfg.AdminAddr
	case httpapi.Internal:
		return cfg.InternalAddr
	}
	return ""
}
