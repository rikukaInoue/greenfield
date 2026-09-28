// Package logger は slog のハンドラを組み立てる唯一の入口。
//
// **このパッケージ以外の core/* はログしない。** 既定ロガーをライブラリから叩くと
// 使う側が出力先も形式もレベルも制御できなくなる。core/* は context からロガーを
// 取るか、エラーを返して呼び出し側に判断を委ねる。
//
// 方針（docs/conventions/internal-06-operations-todo.md §10.1）:
//
//   - 1ストリーム1形式・1出力先。本番は JSON を stdout へ。stderr と混ぜると行の順序が
//     保証されないため、レベルは**フィールド**で表現して出力先を分けない
//   - 時刻は RFC3339 の UTC 固定。slog の既定は実行環境のタイムゾーンで出る
//   - フィールド名は OTel Semantic Conventions に合わせる（service.name 等）。
//     独自語彙で始めると、APM を入れた時に全部マッピングし直すことになる
//   - レベルは実行時に変えられること。LOG_LEVEL で slog.Debug まで出せる
//
// 依存は stdlib だけ。core は「横断的関心事のみ。軽依存に保つ」（internal-01）ため、
// ログのために外部ライブラリを core へ持ち込まない。
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options はロガーの組み立て設定。
type Options struct {
	// Service は OTel の service.name。必須。
	Service string
	// Version は OTel の service.version。
	Version string
	// Env は OTel の deployment.environment.name（development / ci / production）。
	Env string
	// Level は出す下限。既定は Info。
	Level slog.Level
	// Text は人間向けのテキストで出す。**開発機だけの逃げ道**で、既定は JSON。
	Text bool
	// Out は出力先。既定は os.Stdout。
	Out io.Writer
}

// newHandler はハンドラを組む。
//
// 公開していないのは**渡す相手がいないから**。flagd に slog を通す案は実測で潰れた
// （in-process リゾルバはロガーを受け取らない。services/photo/flagsource 参照）。
// 必要になったら公開する。使われない公開APIを先に生やさない。
func newHandler(o Options) slog.Handler {
	out := o.Out
	if out == nil {
		out = os.Stdout
	}
	ho := &slog.HandlerOptions{Level: o.Level, ReplaceAttr: replace}
	var h slog.Handler
	if o.Text {
		h = slog.NewTextHandler(out, ho)
	} else {
		h = slog.NewJSONHandler(out, ho)
	}
	return h.WithAttrs(resourceAttrs(o))
}

// New はハンドラを組んでロガーを返す。
func New(o Options) *slog.Logger { return slog.New(newHandler(o)) }

// FromEnv は環境変数から設定を読んでロガーを返す。合成ルートはこれ1本を呼ぶ。
func FromEnv(service, version string) *slog.Logger {
	return New(Options{
		Service: service,
		Version: version,
		Env:     os.Getenv("ENV"),
		Level:   LevelFromEnv(),
		Text:    strings.EqualFold(os.Getenv("LOG_FORMAT"), "text"),
	})
}

// LevelFromEnv は LOG_LEVEL を読む。未設定・不正値は Info。
// **不正値で落とさない**のは、ログの設定ミスでサービスが起動しないほうが困るため。
func LevelFromEnv() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		return slog.LevelInfo
	}
	return l
}

// resourceAttrs は全行に載る属性。OTel の resource 相当。
func resourceAttrs(o Options) []slog.Attr {
	attrs := make([]slog.Attr, 0, 3)
	if o.Service != "" {
		attrs = append(attrs, slog.String("service.name", o.Service))
	}
	if o.Version != "" {
		attrs = append(attrs, slog.String("service.version", o.Version))
	}
	if o.Env != "" {
		attrs = append(attrs, slog.String("deployment.environment.name", o.Env))
	}
	return attrs
}

// replace は時刻を RFC3339 の UTC に固定する。
//
// slog の既定は**実行環境のタイムゾーン**で出る（実測: `2026/09/28 12:34:12`、
// JSON ハンドラでも +09:00 が付く）。収集側で時刻を揃えるより、出す側で UTC に
// 決めておくほうが安い。精度はミリ秒で切る（ナノ秒まで出しても読む側が使わない）。
func replace(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		return slog.String(slog.TimeKey, a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	return a
}

type ctxKey struct{}

// WithContext は context にロガーを載せる。リクエスト単位の属性（相関ID等）を
// 付けたロガーを下流に渡すために使う。
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext は context のロガーを返す。無ければ既定ロガー。
//
// **無い場合に nil を返さない**。呼び出し側に nil チェックを強いると、忘れた1箇所で
// panic する。ログが出ないことより panic するほうが悪い。
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
