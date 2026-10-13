package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sessionCookie はセッション ID だけを運ぶ Cookie。トークンは入れない（check #17）。
const sessionCookie = "__gf_admin"

// sessionTTL はセッションの寿命。operator の作業は長くても数時間なので短めに置く。
const sessionTTL = 8 * time.Hour

// Session はサーバ側に置く operator の状態。AccessToken はここにしか存在せず、
// ブラウザへは一切返さない。
type Session struct {
	Subject     string
	AAL         int
	AccessToken string
	ExpiresAt   time.Time
}

// sessionStore はプロセス内のセッションストア。
//
// 検証ビルドの admin はタスク 1 つで動く前提なのでメモリで足りる。多重化する日は
// web と同じく外部ストア（Redis / DynamoDB）へ差し替えるが、Cookie に載せるのが
// 「ID + 署名」だけという契約は変えない。
type sessionStore struct {
	secret []byte
	mu     sync.Mutex
	m      map[string]*Session
}

func newSessionStore(secret string) *sessionStore {
	return &sessionStore{secret: []byte(secret), m: map[string]*Session{}}
}

// create はセッションを作り、Cookie に載せる値（id.sig）を返す。
func (s *sessionStore) create(sess *Session) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	sess.ExpiresAt = time.Now().Add(sessionTTL)
	s.mu.Lock()
	s.m[id] = sess
	s.gcLocked()
	s.mu.Unlock()
	return id + "." + s.sign(id), nil
}

// lookup は Cookie 値を検証してセッションを返す。署名不一致・期限切れ・未知は nil。
func (s *sessionStore) lookup(value string) *Session {
	id, sig, ok := strings.Cut(value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.sign(id))) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.m[id]
	if sess == nil {
		return nil
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.m, id)
		return nil
	}
	return sess
}

// destroy はセッションを消す（ログアウト・トークン失効時）。
func (s *sessionStore) destroy(value string) {
	id, _, _ := strings.Cut(value, ".")
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
}

func (s *sessionStore) sign(id string) string {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(id))
	return hex.EncodeToString(h.Sum(nil))
}

// gcLocked は期限切れを掃く。create のたびに呼ぶので、放置されたセッションが溜まらない。
func (s *sessionStore) gcLocked() {
	now := time.Now()
	for id, sess := range s.m {
		if now.After(sess.ExpiresAt) {
			delete(s.m, id)
		}
	}
}

// cookie は Cookie の属性を 1 箇所で決める。httpOnly（JS から読めない）、
// SameSite=Lax（クロスサイトの POST には付かない。状態を変える GET を置かない前提）。
func (s *Server) cookie(value string, maxAge int) *http.Cookie {
	// G124: Secure は TLS 終端の内側で動くかに依存するので設定で決める（SECURE_COOKIE）。
	// 中身はセッション ID + HMAC で、漏れてもトークンではない
	return &http.Cookie{ //nolint:gosec // 上記
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}
