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
	"time"

	_ "github.com/go-sql-driver/mysql" // driver は合成ルートが選ぶ

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/localauthz"
	"github.com/rikukaInoue/greenfield/core/authz/simpleassurance"
	"github.com/rikukaInoue/greenfield/core/authz/staticauthn"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/flags"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/runtimeenv"
	"github.com/rikukaInoue/greenfield/services/photo/blobstore"
	"github.com/rikukaInoue/greenfield/services/photo/flagsource"
	"github.com/rikukaInoue/greenfield/services/photo/handler/admin"
	"github.com/rikukaInoue/greenfield/services/photo/handler/external"
	externalv2 "github.com/rikukaInoue/greenfield/services/photo/handler/external/v2"
	"github.com/rikukaInoue/greenfield/services/photo/handler/internalapi"
	"github.com/rikukaInoue/greenfield/services/photo/readmodel"
	"github.com/rikukaInoue/greenfield/services/photo/repository"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// Version / VersionV2 は各メジャーの info.version。既存のメジャーへ破壊的変更は入れず、
// 次のメジャーのアダプタとして並行提供する（docs/adr/0017）。
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
	usecase.ActionDelete:  "owner",
	usecase.ActionOperate: "operator",
}

// Config はリスナーの待ち受けアドレスと依存先。
type Config struct {
	ExternalAddr string
	InternalAddr string
	AdminAddr    string
	// DSN は photo の業務データ。アプリ実行用のユーザーで接続する。
	DSN string
	// LocalAuthzDSN は擬似ReBAC のタプル置き場。サービスのDBとは別。
	LocalAuthzDSN string
	// Images は画像オブジェクトの置き場所。
	Images blobstore.Config
	// Flags はフィーチャーフラグの取得元。評価はプロセス内で行う（docs/adr/0013）。
	Flags flagsource.Config
}

// ConfigFromEnv は環境変数から設定を読む。
func ConfigFromEnv() Config {
	return Config{
		ExternalAddr:  envOr("PHOTO_EXTERNAL_ADDR", ":8080"),
		InternalAddr:  envOr("PHOTO_INTERNAL_ADDR", ":8081"),
		AdminAddr:     envOr("PHOTO_ADMIN_ADDR", ":8082"),
		DSN:           envOr("PHOTO_DSN", "photo_app:photo_app@tcp(127.0.0.1:3306)/photo?parseTime=true"),
		LocalAuthzDSN: envOr("LOCALAUTHZ_DSN", "localauthz:localauthz@tcp(127.0.0.1:3306)/localauthz"),
		Flags:         flagsource.ConfigFromEnv(),
		Images:        imageConfigFromEnv(),
	}
}

// Deps は組み立て済みの依存一式。
type Deps struct {
	Authenticator authz.Authenticator
	Assurance     authz.AssuranceChecker
	Flags         *flags.Evaluator
	Commands      *usecase.PhotoCommands
	Queries       *usecase.PhotoQueries

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
	store := localauthz.New(authzDB, actionRelations)

	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("photo db: %w", err)
	}
	images, err := blobstore.NewS3Store(ctx, cfg.Images)
	if err != nil {
		return nil, err
	}
	if err := flagsource.Register(ctx, "photo", cfg.Flags); err != nil {
		// フラグ基盤に繋がらなくても起動は続ける。評価は宣言した既定値へ倒れる
		slog.Warn("フラグ基盤に接続できないので既定値で動く", "config", cfg.Flags, "err", err)
	}
	return &Deps{
		Authenticator: authn,
		Assurance:     simpleassurance.New(),
		Flags:         flags.NewEvaluator("photo", flagSet),
		Commands: usecase.NewPhotoCommands(
			consistency.NewAtomic(db), repository.NewPhotoRepository(db), images, store, store, usecase.EnvFaults{}),
		Queries: usecase.NewPhotoQueries(readmodel.NewPhotoReader(db), images, store, store),
		closers: []func() error{db.Close, authzDB.Close},
	}, nil
}

// APIs はリスナー3系統の huma API を組み立てる。スペック生成とサーバ起動で共有する。
// deps が nil なら認証ミドルウェアを付けない（スペック生成専用）。
func APIs(deps *Deps) map[httpapi.Listener]httpapi.API {
	if deps == nil {
		deps = &Deps{}
	}
	base := httpapi.Options{Service: "photo", Version: Version, Authenticator: deps.Authenticator}
	if deps.Flags != nil {
		// 認証の後に置く（ターゲティングキーに Principal を使う）
		base.Middlewares = append(base.Middlewares, deps.Flags.Middleware())
	}

	ext := httpapi.New(httpapi.External, base)
	external.Register(ext, external.Deps{Commands: deps.Commands, Queries: deps.Queries, Assurance: deps.Assurance})
	externalv2.Register(ext.AddMajor(2, VersionV2), externalv2.Deps{Commands: deps.Commands, Queries: deps.Queries, Assurance: deps.Assurance})

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
	deps, err := LocalDeps(ctx, cfg)
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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
