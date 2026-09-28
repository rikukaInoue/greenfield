package middleware_test

// #142 の「確かめること」をそのままテストにしている。
// いずれも「入れる前は測って落ちることを確認した」項目。

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rikukaInoue/greenfield/core/logger"
	"github.com/rikukaInoue/greenfield/core/middleware"
)

// stack は Base を外側から順に適用する（httpapi.New と同じ順）。
func stack(t *testing.T, h http.Handler, out *strings.Builder) http.Handler {
	t.Helper()
	l := logger.New(logger.Options{Service: "svc", Version: "1.0.0", Env: "test", Out: out})
	render := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"internal","status":500}`))
	}
	wrapped := h
	base := middleware.Base(render)
	for i := len(base) - 1; i >= 0; i-- {
		wrapped = base[i](wrapped)
	}
	// ロガーを context に載せる層を最も外に置く（合成ルートの役目）
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r.WithContext(logger.WithContext(r.Context(), l)))
	})
}

func lines(out *strings.Builder) []map[string]any {
	var res []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			continue
		}
		res = append(res, m)
	}
	return res
}

// panic するハンドラに RFC 9457 の 500 が返る（EOF ではない）。
// 入れる前は実測で `Get ...: EOF` だった。
func TestRecoverReturnsProblemNotEOF(t *testing.T) {
	var out strings.Builder
	h := stack(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("ここで落ちる")
	}), &out)

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatalf("接続が切れた（Recover が効いていない）: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	// パニックが slog に出ていて、相関IDが載っている
	var panicLine map[string]any
	for _, m := range lines(&out) {
		if m["msg"] == "panic" {
			panicLine = m
		}
	}
	if panicLine == nil {
		t.Fatalf("panic が slog に出ていない: %s", out.String())
	}
	if panicLine["trace_id"] == nil {
		t.Errorf("panic のログに trace_id が無い: %v", panicLine)
	}
	if s, _ := panicLine["stack"].(string); !strings.Contains(s, "middleware_test") {
		t.Errorf("スタックが入っていない: %q", s)
	}
	// アクセスログ側も 500 を記録している（パニックしたリクエストだけ消えない）
	for _, m := range lines(&out) {
		if m["msg"] == "request" {
			if got := m["http.response.status_code"]; got != float64(500) {
				t.Errorf("アクセスログの status = %v, want 500", got)
			}
			return
		}
	}
	t.Error("パニックしたリクエストのアクセスログが無い")
}

// traceparent を渡すとその trace_id が引き継がれ、span は新規になる。
func TestCorrelateAdoptsIncomingTraceparent(t *testing.T) {
	var out strings.Builder
	var got middleware.Correlation
	h := stack(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = middleware.FromContext(r.Context())
	}), &out)

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentSpan = "00f067aa0ba902b7"
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-"+parentSpan+"-01")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got.TraceID != traceID {
		t.Errorf("trace_id = %q, want %q（引き継げていない）", got.TraceID, traceID)
	}
	if got.ParentSpanID != parentSpan {
		t.Errorf("parent span = %q, want %q", got.ParentSpanID, parentSpan)
	}
	if got.SpanID == parentSpan || len(got.SpanID) != 16 {
		t.Errorf("span_id = %q: サーバ処理は新しい span を持たなければならない", got.SpanID)
	}
	if !got.Sampled {
		t.Error("sampled ビットを落としている")
	}
	if tp := rec.Header().Get("traceparent"); tp != "00-"+traceID+"-"+got.SpanID+"-01" {
		t.Errorf("応答の traceparent = %q", tp)
	}
	// アクセスログに trace_id が乗っている
	for _, m := range lines(&out) {
		if m["msg"] == "request" && m["trace_id"] != traceID {
			t.Errorf("アクセスログの trace_id = %v, want %v", m["trace_id"], traceID)
		}
	}
}

// traceparent が無ければ生成する。
func TestCorrelateGeneratesWhenAbsent(t *testing.T) {
	var out strings.Builder
	var got middleware.Correlation
	h := stack(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = middleware.FromContext(r.Context())
	}), &out)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if len(got.TraceID) != 32 {
		t.Errorf("trace_id = %q, want 32桁", got.TraceID)
	}
	if got.ParentSpanID != "" {
		t.Errorf("親が無いのに parent span がある: %q", got.ParentSpanID)
	}
	// X-Request-Id が来ていなければ trace_id を取っ手として返す
	if rec.Header().Get("X-Request-Id") != got.TraceID {
		t.Errorf("X-Request-Id = %q, want %q", rec.Header().Get("X-Request-Id"), got.TraceID)
	}
}

// 壊れた traceparent は捨てて新規採番する。経路上の壊れたプロキシでサービスを落とさない。
func TestCorrelateIgnoresBrokenTraceparent(t *testing.T) {
	for _, tp := range []string{
		"garbage",
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // 未知バージョン
		"00-zzzz2f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // hex でない
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01", // all-zero は無効
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
	} {
		var out strings.Builder
		var got middleware.Correlation
		h := stack(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got, _ = middleware.FromContext(r.Context())
		}), &out)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("traceparent", tp)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("traceparent=%q で status=%d: 壊れたヘッダで落としてはいけない", tp, rec.Code)
		}
		if len(got.TraceID) != 32 || got.ParentSpanID != "" {
			t.Errorf("traceparent=%q: 採番し直していない（trace=%q parent=%q）", tp, got.TraceID, got.ParentSpanID)
		}
	}
}

// X-Request-Id は外向けの取っ手として往復する。
func TestRequestIDRoundTrips(t *testing.T) {
	var out strings.Builder
	h := stack(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), &out)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "req-abc")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "req-abc" {
		t.Errorf("応答の X-Request-Id = %q, want req-abc", got)
	}
	for _, m := range lines(&out) {
		if m["msg"] == "request" {
			if m["request_id"] != "req-abc" {
				t.Errorf("ログの request_id = %v", m["request_id"])
			}
			return
		}
	}
	t.Error("アクセスログが無い")
}

// **1リクエストにつきログは1行**（canonical log line）。
func TestOneLinePerRequest(t *testing.T) {
	var out strings.Builder
	h := stack(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ハンドラ内の実況は Debug に置く。既定レベル Info では出ない
		logger.FromContext(r.Context()).Debug("処理中の実況")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}), &out)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/photos", nil))

	got := lines(&out)
	if len(got) != 1 {
		t.Fatalf("1リクエストで %d 行出ている（canonical log line に反する）: %s", len(got), out.String())
	}
	m := got[0]
	for k, want := range map[string]any{
		"msg":                       "request",
		"http.request.method":       "POST",
		"url.path":                  "/photos",
		"http.response.status_code": float64(201),
		"http.response.body.size":   float64(2),
		"service.name":              "svc",
	} {
		if m[k] != want {
			t.Errorf("%s = %v, want %v", k, m[k], want)
		}
	}
	if _, ok := m["http.server.request.duration_ms"]; !ok {
		t.Error("所要時間が無い")
	}
}

// ハンドラが出すログにも相関IDが自動で乗る（呼び出し側が毎回書かなくてよい）。
func TestHandlerLogsCarryCorrelation(t *testing.T) {
	var out strings.Builder
	l := logger.New(logger.Options{Service: "svc", Level: slog.LevelDebug, Out: &out})
	render := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }

	var wrapped http.Handler = http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		logger.FromContext(r.Context()).Info("業務イベント", "photo.id", "p1")
	})
	base := middleware.Base(render)
	for i := len(base) - 1; i >= 0; i-- {
		wrapped = base[i](wrapped)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r.WithContext(logger.WithContext(r.Context(), l)))
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	for _, m := range lines(&out) {
		if m["msg"] == "業務イベント" {
			if m["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
				t.Errorf("ハンドラのログに trace_id が乗っていない: %v", m)
			}
			return
		}
	}
	t.Errorf("業務イベントのログが無い: %s", out.String())
}

// 応答を書き始めた後のパニックでは本文を継ぎ足さない（壊れた JSON を作らない）。
func TestRecoverAfterWriteDoesNotAppend(t *testing.T) {
	var out strings.Builder
	h := stack(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
		panic("書いた後に落ちた")
	}), &out)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if body := rec.Body.String(); body != `{"ok":true}` {
		t.Errorf("本文に継ぎ足している: %q", body)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200（ヘッダは送信済み）", rec.Code)
	}
	var sawPanic bool
	for _, m := range lines(&out) {
		if m["msg"] == "panic" {
			sawPanic = true
		}
	}
	if !sawPanic {
		t.Error("ログには残さなければならない")
	}
}

// 秘密を出さない: Authorization / Cookie / クエリ文字列はログに現れない。
func TestNoSecretsInLogs(t *testing.T) {
	var out strings.Builder
	h := stack(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), &out)

	req := httptest.NewRequest(http.MethodGet, "/photos?token=s3cret-in-query", nil)
	req.Header.Set("Authorization", "Bearer s3cret-token")
	req.Header.Set("Cookie", "session=s3cret-cookie")
	h.ServeHTTP(httptest.NewRecorder(), req)

	for _, bad := range []string{"s3cret-token", "s3cret-cookie", "s3cret-in-query", "Bearer"} {
		if strings.Contains(out.String(), bad) {
			t.Errorf("ログに %q が出ている: %s", bad, out.String())
		}
	}
	// パスは出る（クエリは出ない）
	if !strings.Contains(out.String(), `"url.path":"/photos"`) {
		t.Errorf("url.path が出ていない: %s", out.String())
	}
}

// 時刻は RFC3339 の UTC 固定。slog の既定は実行環境のタイムゾーンで出る。
func TestTimestampIsUTCRFC3339(t *testing.T) {
	var out strings.Builder
	logger.New(logger.Options{Service: "svc", Out: &out}).Info("x")

	m := lines(&out)[0]
	ts, _ := m["time"].(string)
	if !strings.HasSuffix(ts, "Z") {
		t.Errorf("time = %q: UTC で終わっていない（実行環境のタイムゾーンが出ている）", ts)
	}
	if len(ts) != len("2006-01-02T15:04:05.000Z") {
		t.Errorf("time = %q: ミリ秒精度の RFC3339 でない", ts)
	}
}

// LOG_LEVEL で Debug が出せる。既定では出ない。
func TestLevelFromEnv(t *testing.T) {
	var quiet, loud strings.Builder
	logger.New(logger.Options{Out: &quiet}).Debug("見えないはず")
	if len(lines(&quiet)) != 0 {
		t.Errorf("既定で Debug が出ている: %s", quiet.String())
	}

	t.Setenv("LOG_LEVEL", "debug")
	logger.New(logger.Options{Level: logger.LevelFromEnv(), Out: &loud}).Debug("見えるはず")
	if len(lines(&loud)) != 1 {
		t.Errorf("LOG_LEVEL=debug で Debug が出ない: %s", loud.String())
	}

	t.Setenv("LOG_LEVEL", "でたらめ")
	if got := logger.LevelFromEnv(); got != slog.LevelInfo {
		t.Errorf("不正値で %v になった。ログの設定ミスでサービスを落とさない", got)
	}
}

// FromContext は無くても nil を返さない。
func TestFromContextNeverNil(t *testing.T) {
	if logger.FromContext(context.Background()) == nil {
		t.Fatal("nil を返した。呼び出し側に nil チェックを強いてはいけない")
	}
}
