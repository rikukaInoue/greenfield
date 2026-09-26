// Package authz は認証・認可・保証レベル検査の差し込み口を定義する。
//
// services/* がimportしてよいのは本パッケージ（interface側）のみで、
// 実装パッケージ（localauthz / oidcauthn / authzhttp 等）のimportは cmd/* に限る。
// 基盤側の型をusecase/handlerへ漏らさないことで、基盤の差し替えの影響を配線部に閉じ込める
// （docs/conventions/internal-04-authz-integration.md §7）。
package authz

import (
	"context"
	"net/http"
	"time"
)

// Authorizer は認可判定（裏: authzサービス / OpenFGA）。
type Authorizer interface {
	Can(ctx context.Context, req Request) (Result, error)
}

// Lister は主体がアクセスできるリソースIDの列挙（認可付き一覧の WHERE IN 用）。
type Lister interface {
	ListAccessible(ctx context.Context, action, resourceType string) ([]string, error)
}

// RelationWriter は関係タプルの書き込み（裏: authzサービス / OpenFGA）。
// Atomic適用第一号であり、usecaseの Atomic.Do の中から呼ぶ。
type RelationWriter interface {
	WriteRelations(ctx context.Context, tuples []Tuple) error
	DeleteRelations(ctx context.Context, tuples []Tuple) error
}

// Authenticator は認証を行う net/http ミドルウェアを提供する。
type Authenticator interface {
	Middleware() func(http.Handler) http.Handler
}

// AssuranceChecker は「どれだけ確かにその人か」を検査する。
// Authorizer とは裏のシステムと差し替え単位が異なるため分けている。
type AssuranceChecker interface {
	Require(ctx context.Context, a Assurance) error
	RequireAAL(ctx context.Context, level AAL) error
}

// Request は認可判定の要求。Action と ResourceType は FGA モデルの relation へ
// マッピングされる契約であり、勝手に増やさない。
type Request struct {
	// Subject は判定対象の主体。空なら ctx の Principal を使う。
	Subject      string
	Action       string
	ResourceType string
	ResourceID   string
	Consistency  Consistency
}

// Result は認可判定の結果。
type Result struct {
	Allowed bool
}

// Consistency は読み取りの鮮度要求。強めるとレイテンシを払う。
type Consistency int

const (
	ConsistencyDefault Consistency = iota
	// ConsistencyHigher は OpenFGA の HIGHER_CONSISTENCY 相当。
	ConsistencyHigher
)

// Tuple は ReBAC の関係データ。Subject / Object の表記は Ref 系ヘルパーで作る。
type Tuple struct {
	Subject  string // 例: user:<sub>
	Relation string // 例: owner
	Object   string // 例: order:<id>
}

// AAL は認証保証レベル。
type AAL int

const (
	AAL1 AAL = 1
	AAL2 AAL = 2
)

// Assurance は保証レベルの要求。
type Assurance struct {
	AAL AAL
	// MaxAge は認証からの経過時間の上限。0 なら無制限。
	MaxAge time.Duration
}
