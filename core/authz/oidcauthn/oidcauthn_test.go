package oidcauthn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
)

// fakeOP は JWKS を配る最小の OP。トークンはテストが署名して作る。
type fakeOP struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
}

func newFakeOP(t *testing.T) *fakeOP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	op := &fakeOP{key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer": op.server.URL, "jwks_uri": op.server.URL + "/certs",
		})
	})
	mux.HandleFunc("/certs", func(w http.ResponseWriter, r *http.Request) {
		pub := op.key.Public().(*rsa.PublicKey) // op.key を見る（ローテーションのテストで差し替わる）
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": op.kid, "kty": "RSA", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
		}}})
	})
	op.server = httptest.NewServer(mux)
	t.Cleanup(op.server.Close)
	return op
}

// sign は claims を RS256 で署名した JWT を返す。alg / kid を差し替えられる。
func (op *fakeOP) sign(t *testing.T, alg, kid string, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	p, _ := json.Marshal(claims)
	head := base64.RawURLEncoding.EncodeToString(h)
	payload := base64.RawURLEncoding.EncodeToString(p)
	digest := sha256.Sum256([]byte(head + "." + payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, op.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return head + "." + payload + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (op *fakeOP) claims(overrides map[string]any) map[string]any {
	c := map[string]any{
		"iss": op.server.URL, "sub": "u-123",
		"exp":   time.Now().Add(time.Minute).Unix(),
		"scope": "openid profile",
	}
	for k, v := range overrides {
		c[k] = v
	}
	return c
}

// serve はミドルウェア越しにリクエストして status と Principal を返す。
func serve(t *testing.T, a *Authenticator, token string) (int, authz.Principal, bool) {
	t.Helper()
	var got authz.Principal
	var ok bool
	h := a.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = authz.PrincipalFrom(r.Context())
	}))
	req := httptest.NewRequest("GET", "/photos", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, got, ok
}

func TestVerifyAndPrincipal(t *testing.T) {
	op := newFakeOP(t)
	a, err := New(context.Background(), op.server.URL)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("人間のトークン", func(t *testing.T) {
		code, p, ok := serve(t, a, op.sign(t, "RS256", op.kid, op.claims(nil)))
		if code != 200 || !ok {
			t.Fatalf("code=%d ok=%v", code, ok)
		}
		if p.Kind != authz.PrincipalUser || p.Subject != "u-123" {
			t.Fatalf("principal: %+v", p)
		}
	})

	t.Run("M2M は client_id で service になる", func(t *testing.T) {
		tok := op.sign(t, "RS256", op.kid, op.claims(map[string]any{
			"client_id": "svc-photo", "scope": "internal:platform",
		}))
		code, p, _ := serve(t, a, tok)
		if code != 200 || p.Kind != authz.PrincipalService || p.ClientID != "svc-photo" {
			t.Fatalf("code=%d principal=%+v", code, p)
		}
		if len(p.Scopes) != 1 || p.Scopes[0] != "internal:platform" {
			t.Fatalf("scopes: %v", p.Scopes)
		}
	})

	t.Run("トークン無しは 401", func(t *testing.T) {
		if code, _, _ := serve(t, a, ""); code != 401 {
			t.Fatalf("code=%d", code)
		}
	})

	t.Run("期限切れは 401", func(t *testing.T) {
		tok := op.sign(t, "RS256", op.kid, op.claims(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}))
		if code, _, _ := serve(t, a, tok); code != 401 {
			t.Fatalf("code=%d", code)
		}
	})

	t.Run("iss 違いは 401", func(t *testing.T) {
		tok := op.sign(t, "RS256", op.kid, op.claims(map[string]any{"iss": "http://evil.example"}))
		if code, _, _ := serve(t, a, tok); code != 401 {
			t.Fatalf("code=%d", code)
		}
	})

	t.Run("alg none / HS256 は 401", func(t *testing.T) {
		for _, alg := range []string{"none", "HS256"} {
			tok := op.sign(t, alg, op.kid, op.claims(nil)) // 署名自体は RS でも alg 宣言で拒否される
			if code, _, _ := serve(t, a, tok); code != 401 {
				t.Fatalf("alg=%s code=%d", alg, code)
			}
		}
	})

	t.Run("署名改竄は 401", func(t *testing.T) {
		tok := op.sign(t, "RS256", op.kid, op.claims(nil))
		parts := strings.Split(tok, ".")
		// ペイロードの sub を書き換える（署名は元のまま）
		var c map[string]any
		raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
		_ = json.Unmarshal(raw, &c)
		c["sub"] = "u-attacker"
		mod, _ := json.Marshal(c)
		parts[1] = base64.RawURLEncoding.EncodeToString(mod)
		if code, _, _ := serve(t, a, strings.Join(parts, ".")); code != 401 {
			t.Fatal("改竄が通っている")
		}
	})

	t.Run("healthz は素通し", func(t *testing.T) {
		h := a.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("code=%d", w.Code)
		}
	})
}

func TestNewRejectsIssuerMismatch(t *testing.T) {
	op := newFakeOP(t)
	// ディスカバリの issuer と設定が食い違う → 起動で落ちる（Tier 1/2 の罠を早期化）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "http://somewhere.else", "jwks_uri": op.server.URL + "/certs"})
	}))
	defer srv.Close()
	if _, err := New(context.Background(), srv.URL); err == nil {
		t.Fatal("issuer 不一致で起動できてしまう")
	} else if !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("err: %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	op := newFakeOP(t)
	a, err := New(context.Background(), op.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	// 鍵をローテーション（kid が変わる）→ 未知 kid で JWKS を引き直して通る
	newKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	op.key, op.kid = newKey, "test-key-2"
	a.mu.Lock()
	a.lastRefresh = time.Now().Add(-time.Minute) // 引き直しのレート制限を外す
	a.mu.Unlock()
	tok := op.sign(t, "RS256", op.kid, op.claims(nil))
	if code, _, _ := serve(t, a, tok); code != 200 {
		t.Fatalf("ローテーション後のトークンが通らない: %d", code)
	}
}

func TestPrincipalAAL(t *testing.T) {
	op := newFakeOP(t)
	a, err := New(context.Background(), op.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for acr, want := range map[string]authz.AAL{"1": authz.AAL1, "2": authz.AAL2, "": authz.AAL1} {
		tok := op.sign(t, "RS256", op.kid, op.claims(map[string]any{"acr": acr}))
		_, p, _ := serve(t, a, tok)
		if p.AAL != want {
			t.Fatalf("acr=%q AAL=%v want %v", acr, p.AAL, want)
		}
	}
}
