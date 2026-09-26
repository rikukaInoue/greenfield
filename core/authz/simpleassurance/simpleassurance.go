// Package simpleassurance は Principal の AAL を突き合わせるだけの AssuranceChecker を提供する。
// 不足時の応答は RFC 9470 に従う。
package simpleassurance

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/problem"
)

// Checker は AAL を突き合わせる AssuranceChecker。
type Checker struct{}

// New は Checker を返す。
func New() *Checker { return &Checker{} }

// RequireAAL は AAL が要求水準に満たなければエラーを返す。
func (c *Checker) RequireAAL(ctx context.Context, level authz.AAL) error {
	return c.Require(ctx, authz.Assurance{AAL: level})
}

// Require は保証レベルを検査する。返したエラーをハンドラがそのまま返すと
// 401 + WWW-Authenticate になる。
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

// insufficient は RFC 9470 の再認証要求を組み立てる。
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

// acr は AAL に対応する ACR 値。OP 側の ACR↔LoA マッピングと揃える。
func acr(l authz.AAL) string {
	switch l {
	case authz.AAL2:
		return "aal2"
	default:
		return "aal1"
	}
}
