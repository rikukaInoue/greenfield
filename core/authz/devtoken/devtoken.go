// Package devtoken はローカル開発・CI用の擬似トークンを組み立て・解読する。
//
// 本番のOP（Keycloak）が発行するJWTの代わりに、署名検証を伴わない自己記述トークンを使う。
// 目的は「認証ミドルウェアとPrincipal伝播を全経路で有効にしたまま、OP稼働前に開発を回す」ことであり、
// 素通しの直叩きを作らないための最小の仕掛けである（conventions/internal-04）。
// 本番での利用は staticauthn の起動時ガードが拒否する。
package devtoken

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
)

// Prefix は擬似トークンの目印。実トークン（JWT）と取り違えないための接頭辞。
const Prefix = "dev."

// Claims は擬似トークンの中身。本番JWTのクレームに対応させ、語彙を先に固定する。
type Claims struct {
	Subject  string   `json:"sub"`
	ClientID string   `json:"client_id,omitempty"`
	Scopes   []string `json:"scope,omitempty"`
	AAL      int      `json:"aal,omitempty"`
	AuthTime int64    `json:"auth_time,omitempty"`
	Service  bool     `json:"svc,omitempty"`
}

var errMalformed = errors.New("devtoken: 形式が不正")

// Mint は擬似トークンを組み立てる。
func Mint(c Claims) string {
	if c.AAL == 0 {
		c.AAL = int(authz.AAL1)
	}
	if c.AuthTime == 0 {
		c.AuthTime = time.Now().Unix()
	}
	b, err := json.Marshal(c)
	if err != nil { // Claims は必ずマーシャルできる
		panic(err)
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(b)
}

// Parse は擬似トークンを Principal へ変換する。
func Parse(token string) (authz.Principal, error) {
	if !strings.HasPrefix(token, Prefix) {
		return authz.Principal{}, errMalformed
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, Prefix))
	if err != nil {
		return authz.Principal{}, errMalformed
	}
	var c Claims
	if err := json.Unmarshal(b, &c); err != nil {
		return authz.Principal{}, errMalformed
	}
	if c.Subject == "" {
		return authz.Principal{}, fmt.Errorf("%w: sub が空", errMalformed)
	}
	kind := authz.PrincipalUser
	if c.Service {
		kind = authz.PrincipalService
	}
	return authz.Principal{
		Subject:  c.Subject,
		Kind:     kind,
		ClientID: c.ClientID,
		Scopes:   c.Scopes,
		AAL:      authz.AAL(c.AAL),
		AuthTime: time.Unix(c.AuthTime, 0),
	}, nil
}
