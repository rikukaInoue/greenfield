// Package httpclient はサービス間呼び出しの HTTP クライアント基盤。
// M2M トークン（client_credentials）の取得・キャッシュ・自動付与を担う。
//
// 担うもの: M2M トークンの取得・キャッシュ・自動付与（3.3）、Idempotency-Key の
// 自動付与と traceparent の伝播（4.1。docs/03-platform.md §9、#138）。
package httpclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rikukaInoue/greenfield/core/middleware"
)

// TokenProvider はアクセストークンの供給元。実運用は TokenSource（client_credentials）、
// ローカル開発・CI は StaticTokenSource（devtoken）を app が選ぶ。
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource は固定トークンを返す（devtoken 用）。
type StaticTokenSource string

// Token は保持している値をそのまま返す。
func (s StaticTokenSource) Token(context.Context) (string, error) { return string(s), nil }

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

// transport は Authorization・Idempotency-Key・traceparent を自動付与する RoundTripper。
type transport struct {
	ts   TokenProvider
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

	// 冪等キー: 変更系は必ず付ける（internal-03 §9「いずれの枝でも冪等キーは必須」）。
	// 呼び出し側が明示したキーは尊重する——リトライで同じキーを送り直すのは呼び出し側の
	// 責任で、ここで毎回新しい UUID を振り直すと重複排除として機能しない（監査 C-7）。
	if mutating(clone.Method) && clone.Header.Get("Idempotency-Key") == "" {
		clone.Header.Set("Idempotency-Key", randomKey())
	}

	// トレース伝播: 受信リクエストの Correlation があれば traceparent で運ぶ
	if c, ok := middleware.FromContext(clone.Context()); ok && c.TraceID != "" {
		clone.Header.Set("traceparent", "00-"+c.TraceID+"-"+c.SpanID+"-01")
	}
	return tr.base.RoundTrip(clone)
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func randomKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Client は TokenProvider のトークン等を自動付与する *http.Client を返す。
func Client(ts TokenProvider) *http.Client {
	return &http.Client{Transport: &transport{ts: ts, base: http.DefaultTransport}, Timeout: 10 * time.Second}
}
