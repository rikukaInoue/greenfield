package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/rikukaInoue/greenfield/core/middleware"
)

func record(t *testing.T) (*httptest.Server, *[]http.Header) {
	t.Helper()
	var got []http.Header
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Clone())
	}))
	t.Cleanup(s.Close)
	return s, &got
}

func TestIdempotencyKeyOnlyOnMutating(t *testing.T) {
	s, got := record(t)
	c := Client(StaticTokenSource("tok"))
	res, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	res, err = c.Post(s.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if k := (*got)[0].Get("Idempotency-Key"); k != "" {
		t.Fatalf("GET に冪等キーが付いている: %q", k)
	}
	if k := (*got)[1].Get("Idempotency-Key"); k == "" {
		t.Fatal("POST に冪等キーが無い")
	}
	if a := (*got)[1].Get("Authorization"); a != "Bearer tok" {
		t.Fatalf("Authorization: %q", a)
	}
}

func TestCallerSetIdempotencyKeyPreserved(t *testing.T) {
	// リトライで同じキーを送り直すのは呼び出し側の責任（C-7）。上書きしたら重複排除が壊れる
	s, got := record(t)
	c := Client(StaticTokenSource("tok"))
	req, _ := http.NewRequest(http.MethodPost, s.URL, nil)
	req.Header.Set("Idempotency-Key", "caller-key")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if k := (*got)[0].Get("Idempotency-Key"); k != "caller-key" {
		t.Fatalf("呼び出し側のキーが上書きされた: %q", k)
	}
}

func TestTraceparentPropagated(t *testing.T) {
	// 受信リクエストを Correlate に通した ctx から外向きに伝播する、実際の経路で確認する
	s, got := record(t)
	c := Client(StaticTokenSource("tok"))
	var ctx context.Context
	capture := middleware.Correlate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	}))
	in := httptest.NewRequest(http.MethodGet, "/", nil)
	in.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-00f067aa0ba902b7-01")
	capture.ServeHTTP(httptest.NewRecorder(), in)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	tp := (*got)[0].Get("traceparent")
	corr, _ := middleware.FromContext(ctx)
	if tp != "00-"+corr.TraceID+"-"+corr.SpanID+"-01" || corr.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("traceparent: %q (corr=%+v)", tp, corr)
	}
}

func TestOriginalRequestNotMutated(t *testing.T) {
	// RoundTripper はリクエストを書き換えない規約。複製に付与していることの確認
	s, _ := record(t)
	c := Client(StaticTokenSource("tok"))
	req, _ := http.NewRequest(http.MethodPost, s.URL, nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if req.Header.Get("Authorization") != "" || req.Header.Get("Idempotency-Key") != "" {
		t.Fatal("元リクエストが書き換えられている")
	}
}

// デッドライン伝播(#268): ctx の残り時間がヘッダで下流へ渡る
func TestDeadlinePropagated(t *testing.T) {
	s, got := record(t)
	c := Client(StaticTokenSource("tok"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	ms, err := strconv.ParseInt((*got)[0].Get(middleware.HeaderRequestTimeout), 10, 64)
	if err != nil || ms <= 0 || ms > 3000 {
		t.Fatalf("%s = %q, want (0, 3000]", middleware.HeaderRequestTimeout, (*got)[0].Get(middleware.HeaderRequestTimeout))
	}
}

// 呼び出し元に締め切りが無くても、クライアント自身の上限(10s)が伝わる。
// http.Client.Timeout は Go の内部でリクエスト ctx に締め切りを付けるため、
// 下流は「このクライアントが待てる時間」より長く粘らない(ヒエラルキーどおり)
func TestClientTimeoutPropagatedWithoutCallerDeadline(t *testing.T) {
	s, got := record(t)
	c := Client(StaticTokenSource("tok"))
	req, _ := http.NewRequest(http.MethodGet, s.URL, nil)
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	ms, err := strconv.ParseInt((*got)[0].Get(middleware.HeaderRequestTimeout), 10, 64)
	if err != nil || ms <= 9000 || ms > 10000 {
		t.Fatalf("%s = %q, want (9000, 10000]", middleware.HeaderRequestTimeout, (*got)[0].Get(middleware.HeaderRequestTimeout))
	}
}

// 残り時間が尽きた呼び出しは送らない(誰も待っていない仕事を下流に始めさせない)
func TestExpiredDeadlineNotSent(t *testing.T) {
	s, got := record(t)
	c := Client(StaticTokenSource("tok"))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	_, err := c.Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if len(*got) != 0 {
		t.Fatalf("尽きた締め切りで %d 回送信された", len(*got))
	}
}
