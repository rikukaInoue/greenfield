// Package oidcauthn は OIDC の JWT を検証する Authenticator。
// issuer のディスカバリから JWKS を引き、RS256 の署名・exp・iss を検査して
// Principal を ctx へ積む。プロダクションの hydraauthn と同じ構造（docs/03-platform.md）。
//
// 外部ライブラリを使わない。検証対象を「既知の issuer が RS256 で署名したトークン」に
// 固定すると、JWT ライブラリが持つ危険な自由度（alg の取り違え等）ごと消えるため、
// stdlib の rsa 検証だけで足りる。alg が RS256 以外なら即拒否する。
package oidcauthn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/problem"
)

// Authenticator は issuer に対する JWT 検証ミドルウェアを提供する。
type Authenticator struct {
	issuer  string
	jwksURI string
	http    *http.Client

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey // kid → 公開鍵
	lastRefresh time.Time
}

// New は issuer（例: http://localhost:8180/realms/greenfield）のディスカバリを引いて
// Authenticator を返す。OP に到達できなければエラー（起動時に配線ミスを落とす）。
func New(ctx context.Context, issuer string) (*Authenticator, error) {
	a := &Authenticator{issuer: strings.TrimRight(issuer, "/"), http: &http.Client{Timeout: 5 * time.Second}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	res, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidcauthn: ディスカバリに到達できない: %w", err)
	}
	defer res.Body.Close()
	var disc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(res.Body).Decode(&disc); err != nil {
		return nil, fmt.Errorf("oidcauthn: ディスカバリを読めない: %w", err)
	}
	if disc.Issuer != a.issuer {
		// issuer の不一致は環境の取り違え（Tier 1/2 の罠。conventions/internal-07）なので起動で落とす
		return nil, fmt.Errorf("oidcauthn: issuer が一致しない: 設定 %q / OP %q", a.issuer, disc.Issuer)
	}
	a.jwksURI = disc.JWKSURI
	if err := a.refreshKeys(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Authenticator) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.jwksURI, nil)
	if err != nil {
		return err
	}
	res, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("oidcauthn: JWKS に到達できない: %w", err)
	}
	defer res.Body.Close()
	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(res.Body).Decode(&set); err != nil {
		return fmt.Errorf("oidcauthn: JWKS を読めない: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	a.mu.Lock()
	a.keys, a.lastRefresh = keys, time.Now()
	a.mu.Unlock()
	return nil
}

// keyFor は kid の鍵を返す。未知の kid は1度だけ JWKS を引き直す（鍵ローテーション対応）。
// 引き直しは10秒に1回まで（壊れたトークンの連投で OP を叩き続けない）。
func (a *Authenticator) keyFor(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	a.mu.Lock()
	k, ok := a.keys[kid]
	stale := time.Since(a.lastRefresh) > 10*time.Second
	a.mu.Unlock()
	if ok {
		return k, nil
	}
	if stale {
		if err := a.refreshKeys(ctx); err != nil {
			return nil, err
		}
		a.mu.Lock()
		k, ok = a.keys[kid]
		a.mu.Unlock()
		if ok {
			return k, nil
		}
	}
	return nil, fmt.Errorf("未知の署名鍵 kid=%q", kid)
}

type claims struct {
	Iss               string          `json:"iss"`
	Sub               string          `json:"sub"`
	Exp               int64           `json:"exp"`
	Nbf               int64           `json:"nbf"`
	Azp               string          `json:"azp"`
	ClientID          string          `json:"client_id"`
	Scope             string          `json:"scope"`
	Acr               string          `json:"acr"`
	AuthTime          int64           `json:"auth_time"`
	PreferredUsername string          `json:"preferred_username"`
	Amr               json.RawMessage `json:"amr"`
}

const leeway = 30 * time.Second

// verify はトークンを検証して claims を返す。
func (a *Authenticator) verify(ctx context.Context, token string) (*claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("JWT の形式でない")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("ヘッダを読めない")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return nil, errors.New("ヘッダを読めない")
	}
	// alg は RS256 に固定する。ここを可変にすると none / HS256 混同の類が全部戻ってくる
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("alg %q は受け付けない（RS256 のみ）", header.Alg)
	}
	key, err := a.keyFor(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("署名を読めない")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errors.New("署名が一致しない")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("ペイロードを読めない")
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, errors.New("クレームを読めない")
	}
	now := time.Now()
	if c.Iss != a.issuer {
		return nil, fmt.Errorf("iss が一致しない: %q", c.Iss)
	}
	if c.Exp != 0 && now.After(time.Unix(c.Exp, 0).Add(leeway)) {
		return nil, errors.New("期限切れ")
	}
	if c.Nbf != 0 && now.Add(leeway).Before(time.Unix(c.Nbf, 0)) {
		return nil, errors.New("まだ有効でない")
	}
	return &c, nil
}

// Middleware は Bearer トークンを検証して Principal を ctx へ積む。
// トークンがない・壊れている・検証に失敗した場合は 401 を返す（素通しは作らない）。
func (a *Authenticator) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" { // 契約外・認証外
				next.ServeHTTP(w, r)
				return
			}
			raw, ok := bearer(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="greenfield"`)
				problem.Write(w, r, problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "アクセストークンが必要"))
				return
			}
			c, err := a.verify(r.Context(), raw)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="greenfield", error="invalid_token"`)
				problem.Write(w, r, problem.New(http.StatusUnauthorized, problem.CodeUnauthenticated, "トークンが不正: "+err.Error()))
				return
			}
			next.ServeHTTP(w, r.WithContext(authz.WithPrincipal(r.Context(), principal(c))))
		})
	}
}

func principal(c *claims) authz.Principal {
	p := authz.Principal{Subject: c.Sub, Kind: authz.PrincipalUser, Scopes: strings.Fields(c.Scope), AAL: authz.AAL1}
	// Keycloak の client_credentials トークンは client_id クレームを持つ（service account）。
	// 人間のトークンには無い。ここで主体の種別を分ける
	p.AuthorizedParty = c.Azp // 人間の対話トークンにも載る。監査で「どのアプリが叩いたか」(#14)
	if c.ClientID != "" {
		p.Kind = authz.PrincipalService
		p.ClientID = c.ClientID
	}
	if c.Acr == "2" || strings.EqualFold(c.Acr, "aal2") {
		p.AAL = authz.AAL2
	}
	if c.AuthTime != 0 {
		p.AuthTime = time.Unix(c.AuthTime, 0)
	}
	return p
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return h[len(prefix):], true
}
