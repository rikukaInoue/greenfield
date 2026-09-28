// Package authzhttp は platform/authz を裏にした Authorizer / Lister / RelationWriter。
// localauthz と差し替えで使う本番アダプタ（docs/03-platform.md、ステージ 3.3）。
//
// action→relation のマッピングは authz サービス側に閉じているので、
// ここは語彙（action / resource type）をそのまま運ぶだけ。
package authzhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
)

// Client は authz サービスへの HTTP アダプタ。
// authz.Authorizer / authz.Lister / authz.RelationWriter を満たす。
type Client struct {
	base string
	http *http.Client
}

// New は base（例: http://localhost:8100）へのアダプタを返す。
// hc には core/httpclient の M2M トークン付きクライアントを渡す（無認証で叩かない）。
func New(base string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{base: base, http: hc}
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("authzhttp: %s: %w", path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("authzhttp: %s: %d: %s", path, res.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// subject は req.Subject が空なら ctx の Principal から引く（localauthz と同じ流儀）。
func subject(ctx context.Context, explicit string) (string, bool) {
	if explicit != "" {
		return explicit, true
	}
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return "", false
	}
	if p.Kind == authz.PrincipalService {
		return authz.ServiceRef(p.ClientID), true
	}
	return authz.UserRef(p.Subject), true
}

func consistency(c authz.Consistency) string {
	if c == authz.ConsistencyHigher {
		return "higher"
	}
	return ""
}

// Can は認可判定。未認証は不許可（通常は認証ミドルウェアが先に 401 にする）。
func (c *Client) Can(ctx context.Context, req authz.Request) (authz.Result, error) {
	sub, ok := subject(ctx, req.Subject)
	if !ok {
		return authz.Result{}, nil
	}
	in := map[string]string{
		"subject": sub, "action": req.Action,
		"resource_type": req.ResourceType, "resource_id": req.ResourceID,
	}
	if v := consistency(req.Consistency); v != "" {
		in["consistency"] = v
	}
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := c.post(ctx, "/check", in, &out); err != nil {
		return authz.Result{}, err
	}
	return authz.Result{Allowed: out.Allowed}, nil
}

// ListAccessible は主体がアクセスできるリソースIDを列挙する（認可付き一覧の WHERE IN 用）。
func (c *Client) ListAccessible(ctx context.Context, action, resourceType string) ([]string, error) {
	sub, ok := subject(ctx, "")
	if !ok {
		return nil, nil
	}
	in := map[string]string{"subject": sub, "action": action, "resource_type": resourceType}
	var out struct {
		ObjectIDs []string `json:"object_ids"`
	}
	if err := c.post(ctx, "/list-objects", in, &out); err != nil {
		return nil, err
	}
	return out.ObjectIDs, nil
}

type tupleJSON struct {
	Subject  string `json:"subject"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

func tuples(ts []authz.Tuple) []tupleJSON {
	out := make([]tupleJSON, len(ts))
	for i, t := range ts {
		out[i] = tupleJSON{Subject: t.Subject, Relation: t.Relation, Object: t.Object}
	}
	return out
}

// WriteRelations はタプルを書く。Atomic 適用第一号として usecase の Atomic.Do の中から
// 呼ばれる契約だが、authz サービス側の書き込みは photo の tx には参加しない
// （失敗すれば usecase がロールバックし、成功後の孤児は無害——check #5 / #6 の前提のまま）。
func (c *Client) WriteRelations(ctx context.Context, ts []authz.Tuple) error {
	return c.post(ctx, "/tuples:write", map[string]any{"writes": tuples(ts)}, nil)
}

// DeleteRelations はタプルを消す。不存在の削除は authz サービス側で無害（自然冪等）。
func (c *Client) DeleteRelations(ctx context.Context, ts []authz.Tuple) error {
	return c.post(ctx, "/tuples:write", map[string]any{"deletes": tuples(ts)}, nil)
}
