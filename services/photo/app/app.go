// Package app は photo サービスの合成ルート。cmd/photo と dev/allinone が共通で使う。
// 差し込み口の実装を import してよいのはここと cmd/*（docs/adr/0003-app-composition-root.md）。
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

	_ "github.com/go-sql-driver/mysql" // driver は合成ルートが選ぶ

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/authzhttp"
	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/authz/localauthz"
	"github.com/rikukaInoue/greenfield/core/authz/oidcauthn"
	"github.com/rikukaInoue/greenfield/core/authz/simpleassurance"
	"github.com/rikukaInoue/greenfield/core/authz/staticauthn"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/flags"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/httpclient"
	"github.com/rikukaInoue/greenfield/core/runtimeenv"
	"github.com/rikukaInoue/greenfield/services/photo/blobstore"
	"github.com/rikukaInoue/greenfield/services/photo/flagsource"
	"github.com/rikukaInoue/greenfield/services/photo/gearlink"
	"github.com/rikukaInoue/greenfield/services/photo/handler/admin"
	externalv2 "github.com/rikukaInoue/greenfield/services/photo/handler/external/v2"
	"github.com/rikukaInoue/greenfield/services/photo/handler/internalapi"
	"github.com/rikukaInoue/greenfield/services/photo/migrations"
	"github.com/rikukaInoue/greenfield/services/photo/readmodel"
	"github.com/rikukaInoue/greenfield/services/photo/repository"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
	"github.com/rikukaInoue/greenfield/telemetry"
)

// Version / VersionV2 は各メジャーの info.version。既存のメジャーへ破壊的変更は入れず、
// 次のメジャーのアダプタとして並行提供する（docs/adr/0017）。
// external のメジャー1は廃止済みで、external は VersionV2 のみ。admin / internal は Version。
const (
	Version   = "1.0.0"
	VersionV2 = "2.0.0"
)

// InternalScope は internal リスナーが要求するスコープ。
const InternalScope = "internal:photo"

// flagSet はこのサービスが評価するフラグの宣言。既定値はフラグ基盤が停止していても
// 安全な側（既存動作）にする。
var flagSet = flags.Set{
	{Name: usecase.FlagDisableUploads, Default: false},
}

// actionRelations は photo の action と FGA モデルの relation の対応。
var actionRelations = localauthz.Mapping{
	usecase.ActionView:    "viewer",
	usecase.ActionEdit:    "editor",
	usecase.ActionPublish: "editor",
	usecase.ActionOperate: "operator",
}

// Config はリスナーの待ち受けアドレスと依存先。
type Config struct {
	ExternalAddr string
	InternalAddr string
	AdminAddr    string
	// MetricsAddr は /metrics の待ち受け(例 :9091)。空なら公開しない(#59)
	MetricsAddr string
	// DSN は photo の業務データ。アプリ実行用のユーザーで接続する。
	DSN string
	// LocalAuthzDSN は擬似ReBAC のタプル置き場。サービスのDBとは別。
	LocalAuthzDSN string
	// Images は画像オブジェクトの置き場所。
	Images blobstore.Config
	// Flags はフィーチャーフラグの取得元。評価はプロセス内で行う（docs/adr/0013）。
	Flags flagsource.Config
	// OIDCIssuer が設定されていれば本番アダプタ（oidcauthn + authzhttp）で組む。
	// 空なら LocalDeps（staticauthn + localauthz。開発用の許可リスト環境のみ）。
	OIDCIssuer string
	// AuthzURL は platform/authz のベースURL。
	AuthzURL string
	// GearInternalURL は gear の internal リスナー。使用機材の紐付け（同期コマンド）に使う。
	GearInternalURL string
	// M2M は authz サービスを呼ぶための client_credentials。
	M2M M2MConfig
}

// M2MConfig はサービス間認証のクライアント資格情報。
type M2MConfig struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	// Scopes は要求するスコープ。authz を呼ぶには internal:platform が要る
	Scopes []string
}

// ConfigFromEnv は環境変数から設定を読む。
func ConfigFromEnv() Config {
	return Config{
		MetricsAddr:     os.Getenv("METRICS_ADDR"),
		ExternalAddr:    envOr("PHOTO_EXTERNAL_ADDR", ":8080"),
		InternalAddr:    envOr("PHOTO_INTERNAL_ADDR", ":8081"),
		AdminAddr:       envOr("PHOTO_ADMIN_ADDR", ":8082"),
		DSN:             envOr("PHOTO_DSN", "photo_app:photo_app@tcp(127.0.0.1:13306)/photo?parseTime=true"),
		LocalAuthzDSN:   envOr("LOCALAUTHZ_DSN", "localauthz:localauthz@tcp(127.0.0.1:13306)/localauthz"),
		Flags:           flagsource.ConfigFromEnv(),
		Images:          imageConfigFromEnv(),
		OIDCIssuer:      os.Getenv("OIDC_ISSUER"),
		AuthzURL:        envOr("AUTHZ_URL", "http://localhost:8100"),
		GearInternalURL: envOr("GEAR_INTERNAL_URL", "http://localhost:8091"),
		M2M: M2MConfig{
			TokenURL:     os.Getenv("M2M_TOKEN_URL"),
			ClientID:     envOr("M2M_CLIENT_ID", "svc-photo"),
			ClientSecret: os.Getenv("M2M_CLIENT_SECRET"),
			Scopes:       []string{"internal:platform"},
		},
	}
}

// Deps は組み立て済みの依存一式。
type Deps struct {
	Authenticator authz.Authenticator
	Assurance     authz.AssuranceChecker
	Flags         *flags.Evaluator
	Commands      *usecase.PhotoCommands
	Queries       *usecase.PhotoQueries

	// DB は業務データのプール。合成ルートが昇格シグナル(プール使用率)の観測に使う(#59)。
	// usecase / handler はこれに触れない(触りたくなったら repository/readmodel の仕事)
	DB *sql.DB

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

// LocalDeps はローカル開発・CI用の実装を組み立てる。
func LocalDeps(ctx context.Context, cfg Config) (*Deps, error) {
	authn, err := staticauthn.New()
	if err != nil {
		return nil, err
	}
	authzDB, err := sql.Open("mysql", cfg.LocalAuthzDSN)
	if err != nil {
		return nil, fmt.Errorf("localauthz store: %w", err)
	}
	authzDB = configureDB(authzDB)
	store := localauthz.New(authzDB, actionRelations)

	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("photo db: %w", err)
	}
	db = configureDB(db)
	images, err := blobstore.NewS3Store(ctx, cfg.Images)
	if err != nil {
		return nil, err
	}
	if err := flagsource.Register(ctx, "photo", cfg.Flags); err != nil {
		// フラグ基盤に繋がらなくても起動は続ける。評価は宣言した既定値へ倒れる
		slog.Warn("フラグ基盤に接続できないので既定値で動く", "config", cfg.Flags, "err", err)
	}
	// dev ループでは gear も staticauthn なので、devtoken の M2M（svc-photo、
	// scope internal:gear）で紐付けコマンドを送る（gear→photo の逆向きと同じ流儀）
	gearToken := httpclient.StaticTokenSource(devtoken.Mint(devtoken.Claims{
		Subject: "svc-photo", ClientID: "svc-photo", Service: true,
		Scopes: []string{"internal:gear"},
	}))
	gear, err := gearlink.New(cfg.GearInternalURL, httpclient.Client(gearToken))
	if err != nil {
		return nil, fmt.Errorf("gearlink: %w", err)
	}
	return &Deps{
		Authenticator: authn,
		Assurance:     simpleassurance.New(),
		Flags:         flags.NewEvaluator("photo", flagSet),
		Commands: usecase.NewPhotoCommands(
			consistency.NewAtomic(db), repository.NewPhotoRepository(db), images, store, store, outboxEventual{}, gear, usecase.EnvFaults{}),
		Queries: usecase.NewPhotoQueries(readmodel.NewPhotoReader(db), images, store, store),
		DB:      db,
		closers: []func() error{db.Close, authzDB.Close},
	}, nil
}

// OIDCDeps は本番アダプタ（oidcauthn + authzhttp）で組み立てる。
// LocalDeps との違いは認証と認可の2依存だけで、usecase / handler には触れない
// （差し替えの影響が配線部に閉じることが internal-04 §7 の主張。check #18）。
func OIDCDeps(ctx context.Context, cfg Config) (*Deps, error) {
	authn, err := oidcauthn.New(ctx, cfg.OIDCIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidcauthn: %w", err)
	}
	if cfg.M2M.TokenURL == "" || cfg.M2M.ClientSecret == "" {
		return nil, fmt.Errorf("authz を呼ぶ M2M 資格情報が未設定（M2M_TOKEN_URL / M2M_CLIENT_SECRET）")
	}
	ts := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, cfg.M2M.Scopes...)
	store := authzhttp.New(cfg.AuthzURL, httpclient.Client(ts))

	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("photo db: %w", err)
	}
	db = configureDB(db)
	images, err := blobstore.NewS3Store(ctx, cfg.Images)
	if err != nil {
		return nil, err
	}
	if err := flagsource.Register(ctx, "photo", cfg.Flags); err != nil {
		slog.Warn("フラグ基盤に接続できないので既定値で動く", "config", cfg.Flags, "err", err)
	}
	// gear internal は internal:gear スコープを要求する。authz 用（internal:platform）とは
	// トークンを分ける——1トークンに全スコープを盛ると、漏れたときの被害が全内部APIに広がる
	gearTS := httpclient.NewTokenSource(cfg.M2M.TokenURL, cfg.M2M.ClientID, cfg.M2M.ClientSecret, "internal:gear")
	gear, err := gearlink.New(cfg.GearInternalURL, httpclient.Client(gearTS))
	if err != nil {
		return nil, fmt.Errorf("gearlink: %w", err)
	}
	return &Deps{
		Authenticator: authn,
		Assurance:     simpleassurance.New(),
		Flags:         flags.NewEvaluator("photo", flagSet),
		Commands: usecase.NewPhotoCommands(
			consistency.NewAtomic(db), repository.NewPhotoRepository(db), images, store, store, outboxEventual{}, gear, usecase.EnvFaults{}),
		Queries: usecase.NewPhotoQueries(readmodel.NewPhotoReader(db), images, store, store),
		DB:      db,
		closers: []func() error{db.Close},
	}, nil
}

// outboxEventual は usecase.Eventual を core/consistency の Outbox で満たすアダプタ。
// usecase は core に依存しないため、型変換だけここで担う。
type outboxEventual struct{ o consistency.Outbox }

func (e outboxEventual) Publish(ctx context.Context, ev usecase.Event) error {
	return e.o.Publish(ctx, consistency.Event{
		ID: ev.ID, Type: ev.Type, AggregateID: ev.AggregateID, Payload: ev.Payload,
	})
}

// APIs はリスナー3系統の huma API を組み立てる。スペック生成とサーバ起動で共有する。
// deps が nil なら認証ミドルウェアを付けない（スペック生成専用）。
func APIs(deps *Deps) map[httpapi.Listener]httpapi.API {
	if deps == nil {
		deps = &Deps{}
	}
	base := httpapi.Options{
		Service: "photo", Version: Version, Authenticator: deps.Authenticator,
		// デプロイ識別子。ECS ではタスク定義リビジョン等を入れる。カナリア観測用（#172）
		Revision: envOr("SERVICE_REVISION", Version),
	}
	if deps.Flags != nil {
		// 認証の後に置く（ターゲティングキーに Principal を使う）
		base.Middlewares = append(base.Middlewares, deps.Flags.Middleware())
	}

	ext := httpapi.New(httpapi.External, base)
	externalv2.Register(ext.AddMajor(2, VersionV2), externalv2.Deps{Commands: deps.Commands, Queries: deps.Queries})

	adm := httpapi.New(httpapi.Admin, base)
	admin.Register(adm, admin.Deps{Commands: deps.Commands, Queries: deps.Queries, Assurance: deps.Assurance})

	intlOpts := base
	intlOpts.RequireScope = InternalScope
	intl := httpapi.New(httpapi.Internal, intlOpts)
	internalapi.Register(intl, internalapi.Deps{Queries: deps.Queries})

	return map[httpapi.Listener]httpapi.API{
		httpapi.External: ext,
		httpapi.Admin:    adm,
		httpapi.Internal: intl,
	}
}

// Run は3リスナーを起動し、ctxのキャンセルまたはいずれかのリスナーの失敗で全て停止する。
func Run(ctx context.Context, cfg Config) error {
	// OIDC_ISSUER の有無で配線を選ぶ。LocalDeps 側は staticauthn / localauthz が
	// それぞれ runtimeenv の許可リストで守られているので、本番で誤って選べば起動で落ちる
	build := LocalDeps
	if cfg.OIDCIssuer != "" {
		build = OIDCDeps
	}
	deps, err := build(ctx, cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	// 昇格シグナルのメトリクス(#59)。契約(OpenAPI)や認証の面と混ぜないため専用ポート。
	// METRICS_ADDR 未設定なら出さない(テスト・スペック生成で余計なリスナーを立てない)
	if cfg.MetricsAddr != "" {
		reg := telemetry.New("photo")
		reg.ObservePool("photo", deps.DB)
		// outbox 滞留(4.2)。未送信の件数と最古の滞留秒。relay が止まる/追いつかない、の
		// 両方がここに出る(check #8 で実測した「relay 停止中は未送信で残る」の常時監視版)。
		// 読めないときは -1(0=滞留なし と 欠測 を混同しない。telemetry.ObserveGauge の規約)
		reg.ObserveGauge("outbox_pending", "未送信イベント数(published_at IS NULL)", func(ctx context.Context) float64 {
			var n float64
			if err := deps.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox WHERE published_at IS NULL").Scan(&n); err != nil {
				return -1
			}
			return n
		})
		reg.ObserveGauge("outbox_oldest_age_seconds", "最古の未送信イベントの経過秒(滞留なしは 0)", func(ctx context.Context) float64 {
			var age sql.NullFloat64
			if err := deps.DB.QueryRowContext(ctx,
				"SELECT TIMESTAMPDIFF(MICROSECOND, MIN(created_at), NOW(6))/1e6 FROM outbox WHERE published_at IS NULL").Scan(&age); err != nil {
				return -1
			}
			if !age.Valid {
				return 0
			}
			return age.Float64
		})
		// contract キューの滞留(#200)。「フラグ100% → 旧経路削除 → contract」の
		// 最後の一歩は人間の起動待ちで、忘れても他のどこにも出ない。0 が定常、
		// 0 より大きい状態が続いたら消化忘れ(min_over_time で警報にする)。読めないときは -1
		reg.ObserveGauge("contract_pending", "未適用の contract マイグレーション数", func(ctx context.Context) float64 {
			n, err := migrations.PendingCount(ctx, deps.DB, migrations.Contract)
			if err != nil {
				return -1
			}
			return float64(n)
		})
		// メトリクスは**排水の間も**見えていてほしい(停止中の観測が消えると、
		// 排水の失敗がどこにも出ない)。リスナーの排水後に止める
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

	// 排水(10.8): 新規受付を止め、処理中のリクエストを待つ。順序は
	// **リスナー → DB** (処理中のクエリを道連れにしない)。上限は ECS の
	// stopTimeout(既定30秒)から SIGKILL までの猶予に収まる 25 秒を既定にする。
	// 排水しきれなかった = 処理中の接続を切った、は黙らせず警告に出す。
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

// imageConfigFromEnv はオブジェクトストレージの設定を読む。
//
// 資格情報とエンドポイントに既定値を置かない。非空の AccessKeyID はスタティック認証を意味するため、
// 既定値を残すと IAM のタスクロール運用（環境変数を設定しない）で既定の認証チェーンが使われなくなる。
// ローカル用の値は開発環境でだけ補う。
func imageConfigFromEnv() blobstore.Config {
	cfg := blobstore.Config{
		Bucket:          os.Getenv("PHOTO_IMAGE_BUCKET"),
		Region:          os.Getenv("AWS_REGION"),
		Endpoint:        os.Getenv("AWS_ENDPOINT_URL"),
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
	}
	if runtimeenv.RequireDevelopment("ローカルのオブジェクトストレージ設定") != nil {
		return cfg
	}
	// 開発環境のみ: compose の RustFS を既定にする
	if cfg.Bucket == "" {
		cfg.Bucket = "photo-images"
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://localhost:9000"
	}
	if cfg.AccessKeyID == "" {
		cfg.AccessKeyID, cfg.SecretAccessKey = "test", "testtest"
	}
	return cfg
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
