package authz

import (
	"context"
	"time"
)

// PrincipalKind は主体の種別。エンドユーザーとサービスを同一の枠組みで扱う。
type PrincipalKind int

const (
	PrincipalUser PrincipalKind = iota + 1
	PrincipalService
)

// Principal は認証済み主体。ミドルウェアが検証結果をctxに積み、
// ハンドラ・usecaseは PrincipalFrom(ctx) のみを参照する。
type Principal struct {
	// Subject はOPのidentity ID。ローカル実装でも不透明な識別子として扱い、
	// 解析や別の値（email等）の代入をしない。
	Subject  string
	Kind     PrincipalKind
	ClientID string
	Scopes   []string
	AAL      AAL
	AuthTime time.Time
}

type principalKey struct{}

// WithPrincipal はPrincipalをctxへ積む。認証ミドルウェアのみが呼ぶ。
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom はctxのPrincipalを返す。未認証ならokがfalse。
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
