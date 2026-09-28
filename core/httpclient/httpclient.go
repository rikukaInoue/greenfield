// Package httpclient はサービス間呼び出しの HTTP クライアント基盤。
// M2M トークン（client_credentials）の取得・キャッシュ・自動付与を担う。
//
// このステージ（3.3）で入るのはトークンまわりのみ。Idempotency-Key の付与と
// トレースID伝播は 4.1（サービス間コマンド）以降で足す（docs/03-platform.md §9、#138）。
package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenSource は client_credentials でトークンを取得し、期限までキャッシュする。
type TokenSource struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       string
	http         *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewTokenSource は Keycloak 等のトークンエンドポイントに対する TokenSource を返す。
// M2M トークンのキャッシュ（期限まで再利用）はクォータ理由がなくても規約として維持する
// （docs/03-platform.md）——毎リクエストの往復は OP を無意味に結合点にする。
func NewTokenSource(tokenURL, clientID, clientSecret string, scopes ...string) *TokenSource {
	return &TokenSource{
		tokenURL: tokenURL, clientID: clientID, clientSecret: clientSecret,
		scopes: strings.Join(scopes, " "),
		http:   &http.Client{Timeout: 5 * time.Second},
	}
}

// 期限の30秒前には取り直す。境界で「取得直後に期限切れ」を踏まないための余白
const expirySlack = 30 * time.Second

// Token は有効なアクセストークンを返す。キャッシュが生きていればネットワークに出ない。
func (t *TokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && time.Now().Before(t.expiry.Add(-expirySlack)) {
		return t.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {t.clientID}, "client_secret": {t.clientSecret}}
	if t.scopes != "" {
		form.Set("scope", t.scopes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := t.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("httpclient: トークン取得: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("httpclient: トークン取得: %d: %s", res.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("httpclient: トークン応答を読めない: %w", err)
	}
	t.token = out.AccessToken
	t.expiry = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return t.token, nil
}

// transport は Authorization を自動付与する RoundTripper。
type transport struct {
	ts   *TokenSource
	base http.RoundTripper
}

func (tr *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := tr.ts.Token(req.Context())
	if err != nil {
		return nil, err
	}
	// RoundTripper はリクエストを書き換えてはいけない規約なので複製する
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+tok)
	return tr.base.RoundTrip(clone)
}

// Client は TokenSource のトークンを自動付与する *http.Client を返す。
func Client(ts *TokenSource) *http.Client {
	return &http.Client{Transport: &transport{ts: ts, base: http.DefaultTransport}, Timeout: 10 * time.Second}
}
