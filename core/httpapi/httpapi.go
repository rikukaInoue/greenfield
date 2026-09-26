// Package httpapi はリスナー（external / admin / internal）ごとの huma API の組み立てを共通化する。
// アプリが知るのは Listen するポートだけであり、TLS・ホスト名・到達制御はインフラの持ち物。
// CORS は全リスナーで閉じる（ブラウザからの直接経路は台帳登録された例外のみ。conventions/external-03）。
package httpapi

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"

	"github.com/rikukaInoue/greenfield/core/problem"
)

// Listener は呼び出し主体ごとのリスナー種別。
// 要求するAAL・レート制限・監査・到達経路が異なるため3系統に分ける（conventions/api-design.md §3.2）。
// パスプレフィックスによる分離は、入口の設定ミス一つで別系統が露出するため採らない。
type Listener string

const (
	External Listener = "external" // 一般ユーザー（Authorization Code + PKCE）
	Admin    Listener = "admin"    // 社内オペレータ
	Internal Listener = "internal" // サービス間（client_credentials）。プライベートドメインのみ
)

// Listeners は OpenAPI 出力順を固定するための一覧。
var Listeners = []Listener{External, Admin, Internal}

// API はリスナー1つ分の huma API と、その HTTP ハンドラ。
type API struct {
	Listener Listener
	Huma     huma.API
	Handler  http.Handler
}

// Options は API の組み立て設定。
type Options struct {
	Service string // サービス名（例: photo）。OpenAPI の title に使う
	Version string // API バージョン（例: 1.0.0）
	// Middlewares は全ルートに適用する net/http 形式のミドルウェア。
	// 認証ミドルウェアは internal も含む全経路に適用する（素通しの直叩きを作らない）。
	Middlewares []func(http.Handler) http.Handler
}

// New はリスナー1つ分の API を作る。ルートの登録は呼び出し側が huma.Register で行う。
func New(l Listener, o Options) API {
	problem.Install() // huma が生成するエラーも code 付きにする（Register より前）

	e := echo.New()
	e.HideBanner, e.HidePort = true, true
	for _, m := range o.Middlewares {
		e.Use(echo.WrapMiddleware(m))
	}

	cfg := huma.DefaultConfig(fmt.Sprintf("%s %s API", o.Service, l), o.Version)
	// スペックとドキュメントはアプリから配らない（api/ にコミットした生成物が唯一の契約置き場）。
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	// SchemasPath を切ったので、応答に `$schema` と Link ヘッダを付ける変換器も外す
	// （解決しないURLを広告しないため）。
	cfg.Transformers = nil
	cfg.CreateHooks = nil
	cfg.Info.Description = description(o.Service, l)

	api := humaecho.NewV4(e, cfg)

	// ヘルスチェックは契約に載せない（OpenAPI から隠す）。
	e.GET("/healthz", func(c echo.Context) error { return c.String(http.StatusOK, "ok\n") })

	return API{Listener: l, Huma: api, Handler: e}
}

func description(service string, l Listener) string {
	switch l {
	case External:
		return fmt.Sprintf("%s サービスの外部公開API（一般ユーザー）。", service)
	case Admin:
		return fmt.Sprintf("%s サービスの管理API（社内オペレータ）。", service)
	case Internal:
		return fmt.Sprintf("%s サービスのサービス間API（client_credentials）。外部からは到達不可。", service)
	}
	return ""
}
