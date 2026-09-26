// Package flags はフィーチャーフラグの評価をリクエスト入口の1回に集約する。
//
// OpenFeature SDK を語彙として直接使い、自前の差し込み口は作らない。
// プロバイダ（flagd 等）は合成ルートが注入する。
package flags

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/open-feature/go-sdk/openfeature"

	"github.com/rikukaInoue/greenfield/core/authz"
)

// Flag は評価するフラグの宣言。Default はフラグ基盤が停止していても安全な側（既存動作）にする。
type Flag struct {
	Name    string
	Default bool
}

// Set は1リクエストで評価するフラグの一覧。サービスが宣言する。
type Set []Flag

// Evaluator は宣言されたフラグをリクエストごとに1回評価する。
type Evaluator struct {
	client *openfeature.Client
	set    Set
}

// NewEvaluator は Evaluator を返す。プロバイダは openfeature.SetProvider で先に登録しておく。
func NewEvaluator(domain string, set Set) *Evaluator {
	return &Evaluator{client: openfeature.NewClient(domain), set: set}
}

type valuesKey struct{}

// Middleware は宣言された全フラグを評価して ctx へ積む。認証ミドルウェアより後に置く
// （ターゲティングキーに Principal を使うため）。
func (e *Evaluator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			next.ServeHTTP(w, r.WithContext(WithValues(ctx, e.Evaluate(ctx))))
		})
	}
}

// Evaluate は宣言された全フラグを評価する。個々の評価が失敗しても宣言した既定値へ倒す。
func (e *Evaluator) Evaluate(ctx context.Context) map[string]bool {
	evalCtx := openfeature.NewEvaluationContext(targetingKey(ctx), attributes(ctx))
	out := make(map[string]bool, len(e.set))
	for _, f := range e.set {
		v, err := e.client.BooleanValue(ctx, f.Name, f.Default, evalCtx)
		if err != nil {
			slog.WarnContext(ctx, "フラグの評価に失敗したので既定値を使う", "flag", f.Name, "default", f.Default, "err", err)
			v = f.Default
		}
		out[f.Name] = v
	}
	return out
}

// WithValues は評価済みの値を ctx へ積む。テストからも使う。
func WithValues(ctx context.Context, values map[string]bool) context.Context {
	return context.WithValue(ctx, valuesKey{}, values)
}

// Bool は評価済みの値を読む。宣言されていないフラグは false を返す。
// ハンドラ・usecase はこの関数しか使わないため、1リクエスト内で値が揺れない。
func Bool(ctx context.Context, name string) bool {
	values, ok := ctx.Value(valuesKey{}).(map[string]bool)
	if !ok {
		return false
	}
	return values[name]
}

// targetingKey は割合展開のキー。同一主体が展開中に ON/OFF を行き来しないよう、
// Principal から取った安定した値を使う。
func targetingKey(ctx context.Context) string {
	if p, ok := authz.PrincipalFrom(ctx); ok {
		return p.Subject
	}
	return ""
}

func attributes(ctx context.Context) map[string]any {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return nil
	}
	return map[string]any{"subject": p.Subject, "kind": int(p.Kind)}
}
