package authz

import (
	"context"
	"time"
)

// PrincipalKind は主体の種別。
type PrincipalKind int

const (
	PrincipalUser PrincipalKind = iota + 1
	PrincipalService
)

// Principal は認証済み主体。ハンドラと usecase は PrincipalFrom でのみ参照する。
type Principal struct {
	// Subject は OP の identity ID。不透明な識別子として扱い、解析や別の値の代入をしない。
	Subject string
	Kind    PrincipalKind
	// ClientID は純 M2M(client_credentials)トークンにだけ載る client_id クレーム。
	ClientID string
	// AuthorizedParty はトークンを取得したクライアントアプリ(azp)。人間の対話トークンにも
	// 載るため、「どのアプリ/エージェントが叩いたか」の監査に使う(#14)。
	AuthorizedParty string
	Scopes          []string
	AAL             AAL
	AuthTime        time.Time
}

type principalKey struct{}

// WithPrincipal は Principal を ctx へ積む。認証ミドルウェアのみが呼ぶ。
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom は ctx の Principal を返す。未認証なら ok が false。
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
