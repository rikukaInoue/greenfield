// Package fga は OpenFGA の HTTP API の薄いクライアント。
//
// SDK を使わないのは、依存を1つ増やすほどの面積を使わないため（5エンドポイント）。
// ここは「OpenFGA の言葉」で話す層であり、action→relation の対応や冪等性の
// 作法は上（サービス本体）が持つ。
package fga

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

// Client は1つのストアに束ねた OpenFGA クライアント。
type Client struct {
	base    string
	http    *http.Client
	storeID string
	modelID string
}

// Tuple は関係タプル。表記は core/authz.Tuple と同じ（user:alice / owner / photo:1）。
type Tuple struct {
	Subject  string
	Relation string
	Object   string
}

// New は base（例: http://localhost:8280）へのクライアントを返す。
func New(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 5 * time.Second}}
}

// StoreID は EnsureStore 後のストアIDを返す（診断用）。
func (c *Client) StoreID() string { return c.storeID }

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return &APIError{Status: res.StatusCode, Body: string(raw)}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// APIError は OpenFGA の非2xx応答。
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("openfga: %d: %s", e.Status, e.Body) }

// EnsureStore は名前でストアを探し、無ければ作る。
func (c *Client) EnsureStore(ctx context.Context, name string) error {
	token := ""
	for {
		var out struct {
			Stores []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"stores"`
			ContinuationToken string `json:"continuation_token"`
		}
		p := "/stores?page_size=100"
		if token != "" {
			p += "&continuation_token=" + url.QueryEscape(token)
		}
		if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
			return fmt.Errorf("list stores: %w", err)
		}
		for _, s := range out.Stores {
			if s.Name == name {
				c.storeID = s.ID
				return nil
			}
		}
		token = out.ContinuationToken
		if token == "" {
			break
		}
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/stores", map[string]string{"name": name}, &created); err != nil {
		return fmt.Errorf("create store: %w", err)
	}
	c.storeID = created.ID
	return nil
}

// EnsureModel はモデルが最新版と一致しなければ書き込む。
// OpenFGA のモデルは追記専用（immutable versions）なので、毎回書くと起動のたびに
// 版が増える。意味比較して差分があるときだけ書く。
func (c *Client) EnsureModel(ctx context.Context, modelJSON string) error {
	var want map[string]any
	if err := json.Unmarshal([]byte(modelJSON), &want); err != nil {
		return fmt.Errorf("model json: %w", err)
	}
	var latest struct {
		AuthorizationModels []map[string]any `json:"authorization_models"`
	}
	if err := c.do(ctx, http.MethodGet, "/stores/"+c.storeID+"/authorization-models?page_size=1", nil, &latest); err != nil {
		return fmt.Errorf("get models: %w", err)
	}
	if len(latest.AuthorizationModels) > 0 {
		got := latest.AuthorizationModels[0]
		id, _ := got["id"].(string)
		delete(got, "id")
		if reflect.DeepEqual(normalize(got), normalize(want)) {
			c.modelID = id
			return nil
		}
	}
	var out struct {
		AuthorizationModelID string `json:"authorization_model_id"`
	}
	if err := c.do(ctx, http.MethodPost, "/stores/"+c.storeID+"/authorization-models", want, &out); err != nil {
		return fmt.Errorf("write model: %w", err)
	}
	c.modelID = out.AuthorizationModelID
	return nil
}

// normalize は JSON 由来の map を比較可能な形に丸める（サーバが省略値を落とす分を吸収）。
func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

type tupleKey struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

func key(t Tuple) tupleKey { return tupleKey{User: t.Subject, Relation: t.Relation, Object: t.Object} }

// Check は判定。higher で HIGHER_CONSISTENCY を要求する。
func (c *Client) Check(ctx context.Context, t Tuple, higher bool) (bool, error) {
	in := map[string]any{"tuple_key": key(t), "authorization_model_id": c.modelID}
	if higher {
		in["consistency"] = "HIGHER_CONSISTENCY"
	}
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := c.do(ctx, http.MethodPost, "/stores/"+c.storeID+"/check", in, &out); err != nil {
		return false, err
	}
	return out.Allowed, nil
}

// ListObjects は主体が relation を持つ object（type:id 形式）を列挙する。
func (c *Client) ListObjects(ctx context.Context, user, relation, typ string, higher bool) ([]string, error) {
	in := map[string]any{"user": user, "relation": relation, "type": typ, "authorization_model_id": c.modelID}
	if higher {
		in["consistency"] = "HIGHER_CONSISTENCY"
	}
	var out struct {
		Objects []string `json:"objects"`
	}
	if err := c.do(ctx, http.MethodPost, "/stores/"+c.storeID+"/list-objects", in, &out); err != nil {
		return nil, err
	}
	return out.Objects, nil
}

// TupleExists は完全一致のタプルが存在するかを read で確かめる。
func (c *Client) TupleExists(ctx context.Context, t Tuple) (bool, error) {
	in := map[string]any{"tuple_key": key(t), "page_size": 1}
	var out struct {
		Tuples []json.RawMessage `json:"tuples"`
	}
	if err := c.do(ctx, http.MethodPost, "/stores/"+c.storeID+"/read", in, &out); err != nil {
		return false, err
	}
	return len(out.Tuples) > 0, nil
}

// Write は writes / deletes を1トランザクションで適用する。どちらも空なら何もしない。
// 既存タプルの write・不存在タプルの delete は OpenFGA がエラーにするため、
// 冪等にしたい呼び出し側は TupleExists で先に絞ること（サービス本体がそれをやる）。
func (c *Client) Write(ctx context.Context, writes, deletes []Tuple) error {
	if len(writes) == 0 && len(deletes) == 0 {
		return nil
	}
	in := map[string]any{"authorization_model_id": c.modelID}
	if len(writes) > 0 {
		ks := make([]tupleKey, len(writes))
		for i, t := range writes {
			ks[i] = key(t)
		}
		in["writes"] = map[string]any{"tuple_keys": ks}
	}
	if len(deletes) > 0 {
		ks := make([]tupleKey, len(deletes))
		for i, t := range deletes {
			ks[i] = key(t)
		}
		in["deletes"] = map[string]any{"tuple_keys": ks}
	}
	return c.do(ctx, http.MethodPost, "/stores/"+c.storeID+"/write", in, nil)
}
