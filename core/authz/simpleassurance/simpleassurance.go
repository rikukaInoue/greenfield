// Package simpleassurance は Principal の AAL を突き合わせるだけの AssuranceChecker。
// ステップアップの実フロー検証（#12）は任意課題であり、ここでは呼び出し語彙
// （RequireAAL / Require）と RFC 9470 の応答形だけを固定する（docs/03-platform.md）。
package simpleassurance

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/problem"
)

// Checker は authz.AssuranceChecker の簡易実装。
type Checker struct{}

// New は Checker を返す。
func New() *Checker { return &Checker{} }

// RequireAAL は Principal の AAL が要求水準に満たなければ、RFC 9470 の 401 を返す。
func (c *Checker) RequireAAL(ctx context.Context, level authz.AAL) error {
	return c.Require(ctx, authz.Assurance{AAL: level})
}

// Require は保証レベルを検査する。不足時のエラーは、ハンドラがそのまま返せば
// 401 + WWW-Authenticate（insufficient_user_authentication）になる。
func (c *Checker) Require(ctx context.Context, a authz.Assurance) error {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "認証が必要")
	}
	if a.AAL > 0 && p.AAL < a.AAL {
		return insufficient(a)
	}
	if a.MaxAge > 0 && !p.AuthTime.IsZero() && time.Since(p.AuthTime) > a.MaxAge {
		return insufficient(a)
	}
	return nil
}

// insufficient は RFC 9470 の再認証要求。acr_values / max_age をクライアントへ伝える。
func insufficient(a authz.Assurance) error {
	challenge := fmt.Sprintf(`Bearer error="insufficient_user_authentication", acr_values="%s"`, acr(a.AAL))
	if a.MaxAge > 0 {
		challenge += fmt.Sprintf(`, max_age="%d"`, int(a.MaxAge.Seconds()))
	}
	err := problem.New(http.StatusUnauthorized, "insufficient_user_authentication",
		fmt.Sprintf("この操作には AAL%d の再認証が必要", a.AAL))
	err.Headers = map[string]string{"WWW-Authenticate": challenge}
	return err
}

// acr は AAL に対応する ACR 値。Keycloak の ACR↔LoA マッピングと揃える（Phase 3.1）。
func acr(l authz.AAL) string {
	switch l {
	case authz.AAL2:
		return "aal2"
	default:
		return "aal1"
	}
}
