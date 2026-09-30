// Package httpapi はリスナーごとの huma API を組み立てる。
// ルータは chi（docs/adr/0001-router-chi.md）。CORS は開けない。
package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/middleware"
	"github.com/rikukaInoue/greenfield/core/problem"
)

// Listener は呼び出し主体ごとのリスナー種別。
type Listener string

const (
	External Listener = "external" // 一般ユーザー
	Admin    Listener = "admin"    // 社内オペレータ
	Internal Listener = "internal" // サービス間
)

// Route は登録されたルート1つ分。
type Route struct {
	Method string
	Path   string
}

// Listeners は OpenAPI の出力順を固定するための一覧。
var Listeners = []Listener{External, Admin, Internal}

// API はリスナー1つ分の huma API と HTTP ハンドラ。
type API struct {
	Listener Listener
	// Huma はメジャー1の API。パスに接頭辞を付けない。
	Huma    huma.API
	Handler http.Handler

	router  chi.Router
	service string
	majors  map[int]huma.API
}

// AddMajor は同じリスナーに、メジャー n（2 以上）の API を並行して作る。
// 戻り値へ登録したパスは /v<n> 配下に置かれ、スペックは別ファイル（<listener>.v<n>.openapi.json）に出る。
// version のメジャーは n と一致させる。
func (a API) AddMajor(n int, version string) huma.API {
	if n < 2 {
		panic(fmt.Sprintf("httpapi: メジャー %d は AddMajor で作らない（1 は New が作る）", n))
	}
	if !strings.HasPrefix(version, fmt.Sprintf("%d.", n)) {
		panic(fmt.Sprintf("httpapi: version %s のメジャーが %d と一致しない", version, n))
	}
	if _, ok := a.majors[n]; ok {
		panic(fmt.Sprintf("httpapi: メジャー %d は作成済み", n))
	}
	// 設定は作り直す。コピーすると OpenAPI（スキーマの登録先）をメジャー間で共有してしまう
	api := humachi.New(a.router, newConfig(a.service, a.Listener, version))
	a.majors[n] = api
	return huma.NewGroup(api, fmt.Sprintf("/v%d", n))
}

// Majors はメジャー番号 → API を返す（1 を含む）。スペックの生成に使う。
func (a API) Majors() map[int]huma.API {
	out := map[int]huma.API{1: a.Huma}
	for n, api := range a.majors {
		out[n] = api
	}
	return out
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

	// Tracing は基盤スタック(相関ID/アクセスログ/復帰)の**直後・認証より前**に適用する
	// 観測用ミドルウェア(サーバ span 等)。認証で落ちる 401 にも span を張るための位置。
	// core は OTel を知らない——合成ルートが telemetry の実装を注入する(#214)。
	Tracing func(http.Handler) http.Handler

	// Revision はこの**デプロイ**の識別子（例: ECS タスク定義リビジョン、イメージタグ）。
	// 全応答に X-Service-Revision として載せる。カナリア・B/G で「どちらの版が
	// 応答したか」を外から観測できないと、重み・切替・ロールバックの検証が成立しない
	// （#172）。空なら Version を使う。
	Revision string
}

// 基盤スタック（相関ID / アクセスログ / パニック復帰）は Options で選ばせず**必ず入れる**。
//
// 選択可能にすると「アクセスログの無いリスナー」が作れてしまう。認証より外側でなければ
// ならない（401 で落ちたリクエストも記録し、認証自身のパニックも拾う）ので、
// Options.Middlewares（認証の後に適用される）では位置が足りない。
//
// これを入れる前は、リスナーは**1リクエストも記録していなかった**（#142 の実測1）。
func baseMiddlewares() []func(http.Handler) http.Handler {
	return middleware.Base(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.New(http.StatusInternalServerError, problem.CodeInternal,
			"内部エラーが発生しました"))
	})
}

// New はリスナー1つ分の API を作る。ルートの登録は呼び出し側が huma.Register で行う。
func New(l Listener, o Options) API {
	problem.Install() // huma が生成するエラーにも code を載せる。Register より前に呼ぶ

	r := chi.NewMux()
	// リビジョンは全応答に載せる（healthz・401 含む）。認証より外に置くのは、
	// カナリア検証がまさに「認証前に落ちる応答」も含めてどちらの版かを見るため
	revision := o.Revision
	if revision == "" {
		revision = o.Version
	}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Service-Revision", revision)
			next.ServeHTTP(w, req)
		})
	})
	// 基盤スタックは認証より外。順序の理由は baseMiddlewares と middleware.Base を参照。
	for _, m := range baseMiddlewares() {
		r.Use(m)
	}
	if o.Tracing != nil {
		r.Use(o.Tracing)
	}
	if o.Authenticator != nil {
		r.Use(o.Authenticator.Middleware())
	}
	if o.RequireScope != "" {
		r.Use(requireScope(o.RequireScope))
	}
	for _, m := range o.Middlewares {
		r.Use(m)
	}

	api := humachi.New(r, newConfig(o.Service, l, o.Version))

	// ヘルスチェックは契約に載せない
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	return API{Listener: l, Huma: api, Handler: r, router: r, service: o.Service, majors: map[int]huma.API{}}
}

func newConfig(service string, l Listener, version string) huma.Config {
	cfg := huma.DefaultConfig(fmt.Sprintf("%s %s API", service, l), version)
	// スペックとドキュメントはアプリから配らない。api/ の生成物が唯一の契約置き場
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	// SchemasPath を切ったので $schema / Link ヘッダの変換器も外す（解決しないURLになる）
	cfg.Transformers = nil
	cfg.CreateHooks = nil
	cfg.Info.Description = description(service, l)
	return cfg
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

// Routes は登録されたルートをメソッドとパスの順に返す。
// 起動時のログや、リスナーごとに別の面が立っていることを検査するテストで使う。
func (a API) Routes() []Route {
	mux, ok := a.Handler.(*chi.Mux)
	if !ok {
		return nil
	}
	var out []Route
	_ = chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, Route{Method: method, Path: route})
		return nil
	})
	slices.SortFunc(out, func(x, y Route) int {
		if c := strings.Compare(x.Path, y.Path); c != 0 {
			return c
		}
		return strings.Compare(x.Method, y.Method)
	})
	return out
}
