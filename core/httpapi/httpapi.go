// Package httpapi はリスナーごとの huma API を組み立てる。
// ルータは chi（docs/adr/0001-router-chi.md）。CORS は開けない。
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
type Listener string

const (
	External Listener = "external" // 一般ユーザー
	Admin    Listener = "admin"    // 社内オペレータ
	Internal Listener = "internal" // サービス間
)

// Listeners は OpenAPI の出力順を固定するための一覧。
var Listeners = []Listener{External, Admin, Internal}

// API はリスナー1つ分の huma API と HTTP ハンドラ。
type API struct {
	Listener Listener
	Huma     huma.API
	Handler  http.Handler
}

// Options は API の組み立て設定。
type Options struct {
	Service string // サービス名（例: photo）。OpenAPI の title に使う
	Version string // API バージョン（例: 1.0.0）

	// Authenticator は認証ミドルウェアの提供元。internal を含む全リスナーに適用する。
	Authenticator authz.Authenticator

	// RequireScope が空でなければ、そのスコープを持たない Principal を 403 で弾く。
	RequireScope string

	// Middlewares は認証の後に適用する。
	Middlewares []func(http.Handler) http.Handler
}

// New はリスナー1つ分の API を作る。ルートの登録は呼び出し側が huma.Register で行う。
func New(l Listener, o Options) API {
	problem.Install() // huma が生成するエラーにも code を載せる。Register より前に呼ぶ

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
	// スペックとドキュメントはアプリから配らない。api/ の生成物が唯一の契約置き場
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	// SchemasPath を切ったので $schema / Link ヘッダの変換器も外す（解決しないURLになる）
	cfg.Transformers = nil
	cfg.CreateHooks = nil
	cfg.Info.Description = description(o.Service, l)

	api := humachi.New(r, cfg)

	// ヘルスチェックは契約に載せない
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
