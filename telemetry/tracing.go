package telemetry

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/rikukaInoue/greenfield/core/middleware"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Tracer は OTLP へのトレース送出(#214 / 10.2)。
//
// 最優先の性質は「**ログの trace_id とトレースの trace_id が同じ**」こと。
// canonical log line(core/middleware)が採番した ID を IDGenerator がそのまま使うので、
// ログの trace_id でトレースを引ける(逆も)。相関の主キーは増えない。
//
// OTEL_EXPORTER_OTLP_ENDPOINT が未設定なら無効: 何も送らず、ミドルウェアは素通し。
// 計測の有無がアプリの動作を変えない(フラグ基盤と同じ「無くても動く」規約)。
type Tracer struct {
	enabled bool
	tp      *sdktrace.TracerProvider
	tracer  trace.Tracer
}

// NewTracer は OTLP エクスポータ付きの Tracer を返す。エンドポイント未設定なら無効の Tracer。
func NewTracer(ctx context.Context, service string) (*Tracer, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return &Tracer{}, nil
	}
	exp, err := otlptracehttp.New(ctx) // エンドポイント等は OTEL_* 環境変数の標準解決
	if err != nil {
		return nil, fmt.Errorf("telemetry: OTLP exporter: %w", err)
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL, semconv.ServiceName(service)))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		// Correlate(core/middleware)が採番した trace_id / span_id をそのまま使う。
		// これが無いと「ログの ID とトレースの ID が別物」になり、相互参照が壊れる
		sdktrace.WithIDGenerator(corrIDGenerator{}),
	)
	// グローバルにも設定する(グローバル参照のライブラリ用)。ただし1プロセス複数サービス
	// では後勝ちになるため、自前の計測は TracerProvider() の明示渡しを使うこと
	otel.SetTracerProvider(tp)
	// W3C traceparent の注入/抽出。propagator は無状態なのでプロセス共有で問題ない
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return &Tracer{enabled: true, tp: tp, tracer: tp.Tracer("greenfield")}, nil
}

// Enabled はトレース送出が有効かを返す。
func (t *Tracer) Enabled() bool { return t.enabled }

// TracerProvider は計測ライブラリ(otelsql 等)へ明示的に渡すためのプロバイダを返す。
// **グローバル(otel.SetTracerProvider)に依存させない**: allinone のように複数サービスが
// 1プロセスに同居すると、グローバルは後勝ちになり span の service.name が別サービスに
// 化ける(実測)。無効時は no-op。
func (t *Tracer) TracerProvider() trace.TracerProvider {
	if !t.enabled {
		return noop.NewTracerProvider()
	}
	return t.tp
}

// Shutdown は未送信の span を吐き切って止める(graceful shutdown の一部として呼ぶ)。
func (t *Tracer) Shutdown(ctx context.Context) error {
	if !t.enabled {
		return nil
	}
	return t.tp.Shutdown(ctx)
}

// Middleware はサーバ span を張る。Correlate の直後(認証より前)に置くこと:
// ID の採番元(Correlation)が ctx に要る + 401 で落ちるリクエストにも span を張るため。
func (t *Tracer) Middleware() func(http.Handler) http.Handler {
	if !t.enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			// 受信 traceparent があれば remote parent として繋ぐ(呼び手の span の子になる)
			if c, ok := middleware.FromContext(ctx); ok && c.ParentSpanID != "" {
				if tid, err := trace.TraceIDFromHex(c.TraceID); err == nil {
					if psid, err := trace.SpanIDFromHex(c.ParentSpanID); err == nil {
						ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
							TraceID: tid, SpanID: psid, Remote: true, TraceFlags: trace.FlagsSampled,
						}))
					}
				}
			}
			// span 名は METHOD + パス。ルートパターン(chi)はこの位置では取れない。
			// 高カーディナリティが問題になったら httpapi 側でパターン名に付け替える
			ctx, span := t.tracer.Start(ctx, r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("url.path", r.URL.Path),
				))
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))
			span.SetAttributes(attribute.Int("http.response.status_code", sw.status))
			span.End()
		})
	}
}

// WrapTransport は HTTP クライアントにクライアント span と traceparent 注入を足す。
// 合成ルートで http.DefaultTransport に適用する(httpclient は素の伝播を持つが、
// こちらが後から上書きするので span の親子が正しく繋がる)。
func (t *Tracer) WrapTransport(base http.RoundTripper) http.RoundTripper {
	if !t.enabled {
		return base
	}
	return otelhttp.NewTransport(base, otelhttp.WithTracerProvider(t.tp))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// corrIDGenerator は Correlation(ログの trace_id / span_id)を span の ID に使う。
type corrIDGenerator struct{}

func (corrIDGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if c, ok := middleware.FromContext(ctx); ok {
		tid, err1 := trace.TraceIDFromHex(c.TraceID)
		sid, err2 := trace.SpanIDFromHex(c.SpanID)
		if err1 == nil && err2 == nil {
			return tid, sid
		}
	}
	// Correlation の外で始まる span(バックグラウンド処理等)は乱数で採番する
	return randTraceID(), randSpanID()
}

func (corrIDGenerator) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	return randSpanID()
}

func randTraceID() trace.TraceID {
	var id trace.TraceID
	for !id.IsValid() {
		_, _ = rand.Read(id[:])
	}
	return id
}

func randSpanID() trace.SpanID {
	var id trace.SpanID
	for !id.IsValid() {
		_, _ = rand.Read(id[:])
	}
	return id
}
