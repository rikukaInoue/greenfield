// Package staticauthn は devtoken を検証する Authenticator を提供する。
// 署名検証はしないが、トークンがなければ 401 を返す（素通しは作らない）。
// 開発用の環境でしか組み立てられない（runtimeenv の許可リスト）。
package staticauthn

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/devtoken"
	"github.com/rikukaInoue/greenfield/core/problem"
	"github.com/rikukaInoue/greenfield/core/runtimeenv"
)

// Authenticator は devtoken を検証する。
type Authenticator struct{}

// New は Authenticator を返す。開発用の環境でなければ誤配線として起動を止める。
// 判定は許可リスト（runtimeenv）で行う。拒否リストでは ENV の未設定や綴り違いが通り抜ける。
func New() (*Authenticator, error) {
	if err := runtimeenv.RequireDevelopment("staticauthn（署名検証のない擬似トークン）"); err != nil {
		return nil, fmt.Errorf("staticauthn: %w（本番は oidcauthn を配線する）", err)
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
