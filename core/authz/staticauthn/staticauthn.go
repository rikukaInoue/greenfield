// Package staticauthn は devtoken を検証する Authenticator を提供する。
// 署名検証はしないが、トークンがなければ 401 を返す（素通しは作らない）。
package staticauthn

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/problem"
)

// Authenticator は devtoken を検証する。
type Authenticator struct{}

// New は Authenticator を返す。ENV=production では誤配線として起動を止める。
func New() (*Authenticator, error) {
	if env := os.Getenv("ENV"); env == "production" || env == "prod" {
		return nil, fmt.Errorf("staticauthn: ENV=%s では使用できない（本番は oidcauthn を配線する）", env)
	}
	return &Authenticator{}, nil
}

// Middleware は Bearer トークンを検証して Principal を ctx へ積む。
// トークンがない、または壊れていれば 401 を返す。
func (a *Authenticator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" { // 契約外・認証外
				next.ServeHTTP(w, r)
				return
			}
			token, ok := bearer(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="greenfield"`)
				problem.Write(w, r, problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "アクセストークンが必要"))
				return
			}
			p, err := devtoken.Parse(token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				problem.Write(w, r, problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "アクセストークンが不正"))
				return
			}
			next.ServeHTTP(w, r.WithContext(authz.WithPrincipal(r.Context(), p)))
		})
	}
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return "", false
	}
	return strings.TrimSpace(h[7:]), true
}
