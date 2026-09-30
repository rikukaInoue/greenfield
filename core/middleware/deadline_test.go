package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// remaining はハンドラに届いた ctx の残り時間を返す。
func remaining(t *testing.T, header string) (time.Duration, int) {
	t.Helper()
	var got time.Duration
	h := Deadline(25 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dl, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("ctx に締め切りが無い")
		}
		got = time.Until(dl)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set(HeaderRequestTimeout, header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return got, rec.Code
}

func TestDeadlineTakesSmaller(t *testing.T) {
	cases := []struct {
		name   string
		header string
		max    time.Duration
		min    time.Duration
	}{
		{"ヘッダ無しは自分の上限", "", 25 * time.Second, 24 * time.Second},
		{"短い残り時間を引き継ぐ", "3000", 3 * time.Second, 2900 * time.Millisecond},
		{"上限より長い残り時間は上限で切る", "60000", 25 * time.Second, 24 * time.Second},
		{"壊れた値は無視して上限", "abc", 25 * time.Second, 24 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, code := remaining(t, c.header)
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}
			if got > c.max || got < c.min {
				t.Fatalf("残り %v, want [%v, %v]", got, c.min, c.max)
			}
		})
	}
}

// 残り時間ゼロで届いたリクエストは処理しない(誰も結果を待っていない)
func TestDeadlineAlreadyExpired(t *testing.T) {
	called := false
	h := Deadline(25 * time.Second)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestTimeout, "0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if called || rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("called=%v status=%d, want ハンドラ未実行 + 504", called, rec.Code)
	}
}
