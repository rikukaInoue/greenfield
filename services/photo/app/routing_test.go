package app_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/services/photo/app"
)

// `:verb` のエンドポイントが plain な `{id}` に食われないことを固定する。
//
// ルータを差し替えるときに最初に壊れる箇所である。Echo のパスパラメータ構文（:id）は
// `:verb` と衝突してパラメータが取れなくなり（docs/adr/0001-router-chi.md）、
// その挙動は 404 ではなく「405 Allow: GET」として現れるため見分けにくい。
// ここで見るのは「ルータがそのパスをハンドラへ振り分けるか」だけなので、deps は nil で足りる。
// 到達したハンドラは nil の依存を触って panic するが、それ自体が「振り分けられた」証拠になる
// （serve が panic を拾って到達として扱う）。
func TestRoutesAreReachable(t *testing.T) {
	apis := app.APIs(nil)

	cases := []struct {
		listener httpapi.Listener
		method   string
		path     string
	}{
		// external はメジャー2のみ（docs/adr/0017。メジャー1は廃止済み）
		{httpapi.External, http.MethodPost, "/v2/photos"},
		{httpapi.External, http.MethodGet, "/v2/photos"},
		{httpapi.External, http.MethodGet, "/v2/photos/1"},
		{httpapi.External, http.MethodPost, "/v2/photos/1:commit"},
		{httpapi.External, http.MethodPost, "/v2/photos/1:publish"},
		{httpapi.Admin, http.MethodGet, "/photos"},
		{httpapi.Admin, http.MethodPost, "/accounts/alice:delete"},
		{httpapi.Internal, http.MethodGet, "/photos/1"},
		{httpapi.Internal, http.MethodGet, "/gear-items/1/photos"},
	}
	for _, c := range cases {
		t.Run(string(c.listener)+" "+c.method+" "+c.path, func(t *testing.T) {
			if got := serve(t, apis, c.listener, c.method, c.path); !got.reached {
				t.Fatalf("status = %d（ルートに到達していない。Allow=%q body=%s）", got.code, got.allow, got.body)
			}
		})
	}
}

// リスナーごとに別の面が立っていることを固定する。
// パスプレフィックスではなくポートで分ける設計なので、他系統のパスは存在しない。
//
// HTTP では判定できない。internal はスコープ要求のミドルウェアがルーティングより先に走り、
// 存在しないパスにも 401 を返すためである。ルート表を直接見る。
func TestListenersDoNotShareRoutes(t *testing.T) {
	apis := app.APIs(nil)

	want := map[httpapi.Listener][]httpapi.Route{
		httpapi.External: {
			{Method: "GET", Path: "/healthz"},
			{Method: "GET", Path: "/v2/photos"},
			{Method: "POST", Path: "/v2/photos"},
			{Method: "GET", Path: "/v2/photos/{id}"},
			{Method: "POST", Path: "/v2/photos/{id}:commit"},
			{Method: "POST", Path: "/v2/photos/{id}:publish"},
		},
		httpapi.Admin: {
			{Method: "POST", Path: "/accounts/{subject}:delete"},
			{Method: "GET", Path: "/healthz"},
			{Method: "GET", Path: "/photos"},
		},
		httpapi.Internal: {
			{Method: "GET", Path: "/gear-items/{gear_item_id}/photos"},
			{Method: "GET", Path: "/healthz"},
			{Method: "GET", Path: "/photos/{id}"},
		},
	}

	for _, l := range httpapi.Listeners {
		t.Run(string(l), func(t *testing.T) {
			got := apis[l].Routes()
			if len(got) == 0 {
				t.Fatal("ルートが取れない（ルータが chi ではない可能性）")
			}
			if !slices.Equal(got, want[l]) {
				t.Errorf("ルートが期待と違う\n got: %v\nwant: %v", got, want[l])
			}
		})
	}
}

// 廃止したメジャー1のパスには届かない（docs/adr/0017）。
func TestRetiredMajorIsGone(t *testing.T) {
	apis := app.APIs(nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/photos"},
		{http.MethodPost, "/photos"},
		{http.MethodGet, "/photos/1"},
		{http.MethodPost, "/photos/1:commit"},
	} {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			if got := serve(t, apis, httpapi.External, c.method, c.path); got.reached {
				t.Fatalf("廃止した v1 のパスに到達した（status=%d）", got.code)
			}
		})
	}
}

// ヘルスチェックは全リスナーで応答し、契約には載らない。
func TestHealthzOnEveryListener(t *testing.T) {
	apis := app.APIs(nil)
	for _, l := range httpapi.Listeners {
		t.Run(string(l), func(t *testing.T) {
			if got := serve(t, apis, l, http.MethodGet, "/healthz"); got.code != http.StatusOK {
				t.Fatalf("status = %d, want 200", got.code)
			}
		})
	}
}

// スペックとドキュメントはアプリから配らない（api/ の生成物が唯一の契約置き場）。
func TestSpecIsNotServed(t *testing.T) {
	apis := app.APIs(nil)
	for _, path := range []string{"/openapi.json", "/openapi.yaml", "/docs", "/schemas/Error.json"} {
		t.Run(path, func(t *testing.T) {
			if got := serve(t, apis, httpapi.External, http.MethodGet, path); got.code != http.StatusNotFound {
				t.Fatalf("%s の status = %d, want 404", path, got.code)
			}
		})
	}
}

// CORS はどのリスナーでも開けない。
func TestCORSIsClosed(t *testing.T) {
	apis := app.APIs(nil)
	for _, l := range httpapi.Listeners {
		t.Run(string(l), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.Header.Set("Origin", "https://evil.example")
			rec := httptest.NewRecorder()
			apis[l].Handler.ServeHTTP(rec, req)
			for name := range rec.Header() {
				if len(name) > 21 && name[:21] == "Access-Control-Allow-" {
					t.Errorf("%s が返っている", name)
				}
			}
		})
	}
}

// result はリクエストの結果。reached は「ルータがハンドラへパスパラメータ付きで振り分けたか」。
type result struct {
	code    int
	allow   string
	body    string
	reached bool
}

// serve はリクエストを1つ流す。deps が nil のためハンドラは panic しうるが、
// panic はルートに到達した証拠なので拾って reached=true として返す。
func serve(t *testing.T, apis map[httpapi.Listener]httpapi.API, l httpapi.Listener, method, path string) result {
	t.Helper()
	api, ok := apis[l]
	if !ok {
		t.Fatalf("リスナー %s が組み立てられていない", l)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)

	reached := true
	func() {
		defer func() { _ = recover() }()
		api.Handler.ServeHTTP(rec, req)
		// panic せずに返った場合、404 / 405 ならルートが無い。
		// パスパラメータが届かなかった場合も到達とみなさない: `:verb` を扱えないルータでは
		// パスが一致しても `{id}` が取れず、huma が 422 と path.id の欠落を返す
		// （docs/adr/0001-router-chi.md の Echo の症状そのもの）
		switch {
		case rec.Code == http.StatusNotFound, rec.Code == http.StatusMethodNotAllowed:
			reached = false
		case rec.Code == http.StatusUnprocessableEntity && strings.Contains(rec.Body.String(), `"location":"path.`):
			reached = false
		}
	}()
	return result{code: rec.Code, allow: rec.Header().Get("Allow"), body: rec.Body.String(), reached: reached}
}
