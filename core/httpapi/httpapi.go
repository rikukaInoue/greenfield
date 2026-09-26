// Package httpapi はリスナー（external / admin / internal）ごとの huma API の組み立てを共通化する。
//
// ルータは chi を使う。規約（conventions/api-design.md §3.1）は Echo + humaecho を指定しているが、
// Echo のパスパラメータ構文（:id）は同じ規約が定める `:verb`（AIP-136 のカスタムメソッド、
// 例 POST /photos/{id}:publish）と衝突し、パスパラメータが取れなくなる（v4 / v5 とも 422）。
// 両立しないため、API設計の中核である `:verb` を採り、ルータを chi に替えた。→ 還流事項。
// アプリが知るのは Listen するポートだけであり、TLS・ホスト名・到達制御はインフラの持ち物。
// CORS は全リスナーで閉じる（ブラウザからの直接経路は台帳登録された例外のみ。conventions/external-03）。
package httpapi

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/rikukaInoue/greenfield/core/authz"
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

	// Authenticator は認証ミドルウェアの提供元。**internal を含む全リスナーに適用する**。
	// 「プライベートネットワークだから無認証」は試作でも採らない（#27）。
	Authenticator authz.Authenticator

	// RequireScope が空でなければ、そのスコープを持たない Principal を 403 で弾く。
	// internal リスナーに `internal:<service>` を要求するために使う。
	RequireScope string

	// Middlewares は認証の後に適用する net/http 形式のミドルウェア。
	Middlewares []func(http.Handler) http.Handler
}

// New はリスナー1つ分の API を作る。ルートの登録は呼び出し側が huma.Register で行う。
func New(l Listener, o Options) API {
	problem.Install() // huma が生成するエラーも code 付きにする（Register より前）

	r := chi.NewMux()
	if o.Authenticator != nil {
		r.Use(o.Authenticator.Middleware())
	}
	if o.RequireScope != "" {
		r.Use(requireScope(o.RequireScope))
	}
	for _, m := range o.Middlewares {
		r.Use(m)
	}

	cfg := huma.DefaultConfig(fmt.Sprintf("%s %s API", o.Service, l), o.Version)
	// スペックとドキュメントはアプリから配らない（api/ にコミットした生成物が唯一の契約置き場）。
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	// SchemasPath を切ったので、応答に `$schema` と Link ヘッダを付ける変換器も外す
	// （解決しないURLを広告しないため）。
	cfg.Transformers = nil
	cfg.CreateHooks = nil
	cfg.Info.Description = description(o.Service, l)

	api := humachi.New(r, cfg)

	// ヘルスチェックは契約に載せない（OpenAPI から隠す）。
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	return API{Listener: l, Huma: api, Handler: r}
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

// requireScope は Principal が scope を持たなければ 403 を返す。
// 認証（誰か）を通っても、そのトークンにこのAPIを呼ぶ権限がなければ入れない。
func requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			p, ok := authz.PrincipalFrom(r.Context())
			if !ok {
				problem.Write(w, r, problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "認証が必要"))
				return
			}
			if !slices.Contains(p.Scopes, scope) {
				problem.Write(w, r, problem.New(http.StatusForbidden, problem.CodeForbidden,
					fmt.Sprintf("スコープ %s が必要", scope)))
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}
