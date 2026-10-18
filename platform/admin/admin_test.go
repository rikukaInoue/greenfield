package admin_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/rikukaInoue/greenfield/core/authz/devtoken"

	"github.com/rikukaInoue/greenfield/platform/admin"
)

// tokenLike は web の e2e/token-exposure.spec と同じ正規表現。devtoken（dev. 接頭辞）と
// JWT（eyJ… の3分割）の両方を拾う。
var tokenLike = regexp.MustCompile(`dev\.[A-Za-z0-9+/=_-]{16,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)

// fakeAdminAPI は photo の admin リスナーのスタブ。本物と同じ判定を最小限で再現する:
// トークン無しは 401、AAL1 での削除は RFC 9470 のチャレンジ、operator 以外は 403。
type fakeAdminAPI struct {
	deleted []string
}

func (f *fakeAdminAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /photos", func(w http.ResponseWriter, r *http.Request) {
		p, ok := f.principal(w, r)
		if !ok {
			return
		}
		if p.Subject != "carol" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"photos": []map[string]any{
			{"id": 1, "owner_id": "alice", "caption": "朝の光", "visibility": "private", "status": "ready", "created_at": "2026-10-11T01:02:03Z"},
			{"id": 2, "owner_id": "bob", "caption": "夕景", "visibility": "public", "status": "pending_upload", "created_at": "2026-10-11T04:05:06Z"},
		}})
	})
	mux.HandleFunc("POST /accounts/{subject}", func(w http.ResponseWriter, r *http.Request) {
		// chi 側は /accounts/{subject}:delete。ServeMux は ":delete" を含めて subject に取り込むので剥がす
		subject := strings.TrimSuffix(r.PathValue("subject"), ":delete")
		p, ok := f.principal(w, r)
		if !ok {
			return
		}
		if p.AAL < 2 {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_user_authentication", acr_values="aal2"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if p.Subject != "carol" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.deleted = append(f.deleted, subject)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"deleted_photos": 3})
	})
	return mux
}

func (f *fakeAdminAPI) principal(w http.ResponseWriter, r *http.Request) (devtoken.Claims, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" || !strings.HasPrefix(tok, devtoken.Prefix) {
		w.WriteHeader(http.StatusUnauthorized)
		return devtoken.Claims{}, false
	}
	p, err := devtoken.Parse(tok)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return devtoken.Claims{}, false
	}
	return devtoken.Claims{Subject: p.Subject, AAL: int(p.AAL)}, true
}

// recorder は全応答（ヘッダと本文）を控えて、後からトークン形状を探すための transport。
type recorder struct {
	base      http.RoundTripper
	responses []string
}

func (rec *recorder) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := rec.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	_ = res.Header.Write(&sb)
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	sb.Write(body)
	rec.responses = append(rec.responses, sb.String())
	res.Body = io.NopCloser(strings.NewReader(string(body)))
	return res, nil
}

type harness struct {
	t      *testing.T
	api    *fakeAdminAPI
	ui     *httptest.Server
	client *http.Client
	rec    *recorder
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("ENV", "test")
	api := &fakeAdminAPI{}
	apiSrv := httptest.NewServer(api.handler())
	t.Cleanup(apiSrv.Close)
	srv, err := admin.New(admin.Config{PhotoAdminURL: apiSrv.URL, SessionSecret: "test-secret", DevLogin: true})
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(srv.Handler())
	t.Cleanup(ui.Close)
	jar, _ := cookiejar.New(nil)
	rec := &recorder{base: http.DefaultTransport}
	return &harness{t: t, api: api, ui: ui, client: &http.Client{Jar: jar, Transport: rec}, rec: rec}
}

func (h *harness) get(path string) *http.Response {
	h.t.Helper()
	res, err := h.client.Get(h.ui.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

func (h *harness) post(path string, form url.Values) *http.Response {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.ui.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return string(b)
}

func (h *harness) login(subject string, aal string, returnTo string) *http.Response {
	return h.post("/login", url.Values{"subject": {subject}, "aal": {aal}, "returnTo": {returnTo}})
}

// 一連の操作（ログイン→一覧→削除の確認→ステップアップ→削除完遂）の全応答にトークン形状が無い。
// check #17 の admin 版: トークンはサーバ側セッションにしか居ない。
func TestTokenNeverReachesBrowser(t *testing.T) {
	h := newHarness(t)

	res := h.login("carol", "1", "/")
	if got := body(t, res); !strings.Contains(got, "朝の光") || !strings.Contains(got, "夕景") {
		t.Fatalf("ログイン後の一覧に写真が無い: %.300s", got)
	}
	body(t, h.get("/?owner_id=alice&limit=10"))
	body(t, h.get("/accounts/alice/delete"))
	// AAL1 での削除 → ステップアップへ誘導 → AAL2 で入り直し → 確認画面に戻る → 削除
	res = h.post("/accounts/alice:delete", nil)
	got := body(t, res)
	if !strings.Contains(got, "再認証") {
		t.Fatalf("AAL1 の削除がステップアップへ誘導されない: status=%d body=%.300s", res.StatusCode, got)
	}
	res = h.login("carol", "2", "/accounts/alice/delete")
	if got := body(t, res); !strings.Contains(got, "削除を実行する") {
		t.Fatalf("ステップアップ後に確認画面へ戻らない: %.300s", got)
	}
	res = h.post("/accounts/alice:delete", nil)
	if got := body(t, res); !strings.Contains(got, "写真 3 件") {
		t.Fatalf("削除が完遂しない: status=%d body=%.300s", res.StatusCode, got)
	}
	if len(h.api.deleted) != 1 || h.api.deleted[0] != "alice" {
		t.Fatalf("API に届いた削除: %v", h.api.deleted)
	}

	if len(h.rec.responses) < 8 {
		t.Fatalf("応答が %d 件しか記録されていない（リダイレクトを含めて8以上あるはず）", len(h.rec.responses))
	}
	for i, r := range h.rec.responses {
		if tokenLike.MatchString(r) {
			t.Errorf("応答 %d にトークン形状の文字列がある:\n%.400s", i, r)
		}
		if strings.Contains(r, "accessToken") || strings.Contains(r, "AccessToken") {
			t.Errorf("応答 %d にトークンのフィールド名がある", i)
		}
	}
	// Cookie はセッション ID と署名だけ（64 hex . 64 hex）
	for _, c := range h.client.Jar.Cookies(mustURL(h.ui.URL)) {
		if c.Name != "__gf_admin" {
			t.Errorf("想定外の Cookie: %s", c.Name)
		}
		if !regexp.MustCompile(`^[0-9a-f]{48}\.[0-9a-f]{64}$`).MatchString(c.Value) {
			t.Errorf("Cookie がセッション ID + 署名の形でない: %.40s…", c.Value)
		}
	}
}

// 認証なしは一覧へ届かずログインへ送られ、戻り先が保持される。
func TestUnauthenticatedIsRedirectedToLogin(t *testing.T) {
	h := newHarness(t)
	h.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res := h.get("/accounts/alice/delete")
	body(t, res)
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "returnTo=%2Faccounts%2Falice%2Fdelete") {
		t.Fatalf("status=%d location=%s", res.StatusCode, res.Header.Get("Location"))
	}
}

// 他人（operator タプルの無い subject）は API の 403 がそのまま画面に出る。画面側に権限表は無い。
func TestNonOperatorSeesForbidden(t *testing.T) {
	h := newHarness(t)
	res := h.login("mallory", "2", "/")
	if got := body(t, res); res.StatusCode != http.StatusForbidden || !strings.Contains(got, "operator") {
		t.Fatalf("status=%d body=%.300s", res.StatusCode, got)
	}
}

// クロスサイトからの POST は Fetch Metadata で弾く（Cookie が Lax で届かないことの二重化）。
func TestCrossSitePostIsRejected(t *testing.T) {
	h := newHarness(t)
	body(t, h.login("carol", "2", "/"))
	req, _ := http.NewRequest(http.MethodPost, h.ui.URL+"/accounts/alice:delete", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body(t, res)
	if res.StatusCode != http.StatusForbidden || len(h.api.deleted) != 0 {
		t.Fatalf("status=%d deleted=%v", res.StatusCode, h.api.deleted)
	}
}

// 防御ヘッダ。JS を配らないので script-src は 'none'（web の 'unsafe-inline' の妥協が無い）。
func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	res := h.get("/login")
	body(t, res)
	csp := res.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP に %s が無い: %s", want, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP が script を許している: %s", csp)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers: %v", res.Header)
	}
}

// 開発用ログインは許可リストの環境以外では組み立てられない（本番で擬似トークンが生えない）。
func TestDevLoginRefusedInProd(t *testing.T) {
	t.Setenv("ENV", "prod")
	if _, err := admin.New(admin.Config{PhotoAdminURL: "http://localhost:1", SessionSecret: "x", DevLogin: true}); err == nil {
		t.Fatal("ENV=prod で DevLogin が通ってしまった")
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
