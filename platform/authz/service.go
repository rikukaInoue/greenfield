// Package authz は認可サービス本体。API 契約は docs/03-platform.md のとおり
// POST /check・/batch-check・/list-objects・/tuples:write の4つ。
//
// action→relation のマッピングはこのサービス内に閉じ、プロダクトには語彙
// （action 名・resource type 名）だけを見せる。relation 名がプロダクトへ漏れると、
// FGA モデルの変更がプロダクトのデプロイと結合してしまう。
package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rikukaInoue/greenfield/core/problem"

	"github.com/rikukaInoue/greenfield/platform/authz/fga"
)

// Model は FGA 最小モデル（docs/03-platform.md）。
// viewer / editor は直接割り当てず、owner か parent の operator から導出する。
const Model = `{
  "schema_version": "1.1",
  "type_definitions": [
    {"type": "user"},
    {
      "type": "platform",
      "relations": {"operator": {"this": {}}, "support": {"this": {}}},
      "metadata": {"relations": {
        "operator": {"directly_related_user_types": [{"type": "user"}]},
        "support":  {"directly_related_user_types": [{"type": "user"}]}
      }}
    },
    {
      "type": "photo",
      "relations": {
        "parent": {"this": {}},
        "owner":  {"this": {}},
        "viewer": {"union": {"child": [
          {"computedUserset": {"relation": "owner"}},
          {"tupleToUserset": {"tupleset": {"relation": "parent"}, "computedUserset": {"relation": "operator"}}}
        ]}},
        "editor": {"union": {"child": [
          {"computedUserset": {"relation": "owner"}},
          {"tupleToUserset": {"tupleset": {"relation": "parent"}, "computedUserset": {"relation": "operator"}}}
        ]}}
      },
      "metadata": {"relations": {
        "parent": {"directly_related_user_types": [{"type": "platform"}]},
        "owner":  {"directly_related_user_types": [{"type": "user"}]}
      }}
    }
  ]
}`

// DefaultMapping は action → relation の対応。photo 側の語彙（usecase の Action*）と
// 揃っていることが契約で、勝手に増やさない（core/authz.Request のコメント参照）。
var DefaultMapping = map[string]string{
	"photo.view":       "viewer",
	"photo.edit":       "editor",
	"photo.publish":    "editor",
	"platform.operate": "operator",
}

// FGA はサービスが必要とする OpenFGA 操作。実体は fga.Client。
type FGA interface {
	Check(ctx context.Context, t fga.Tuple, higher bool) (bool, error)
	ListObjects(ctx context.Context, user, relation, typ string, higher bool) ([]string, error)
	TupleExists(ctx context.Context, t fga.Tuple) (bool, error)
	Write(ctx context.Context, writes, deletes []fga.Tuple) error
}

// Server は http.Handler。
type Server struct {
	fga     FGA
	mapping map[string]string
	mux     *http.ServeMux
}

// NewServer は4エンドポイント + /healthz を持つハンドラを返す。
func NewServer(f FGA, mapping map[string]string) *Server {
	s := &Server{fga: f, mapping: mapping, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	s.mux.HandleFunc("POST /check", s.check)
	s.mux.HandleFunc("POST /batch-check", s.batchCheck)
	s.mux.HandleFunc("POST /list-objects", s.listObjects)
	s.mux.HandleFunc("POST /tuples:write", s.tuplesWrite)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// CheckRequest は1件の判定要求。
type CheckRequest struct {
	Subject      string `json:"subject"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	// Consistency は "" / "default" / "higher"。higher は書き込み直後の可視性を要求する
	// （対価はレイテンシ。internal-03 の読み取り鮮度の対概念）。
	Consistency string `json:"consistency,omitempty"`
}

func (cr CheckRequest) validate(mapping map[string]string) (rel string, higher bool, err error) {
	if !strings.Contains(cr.Subject, ":") {
		return "", false, fmt.Errorf("subject は type:id 形式（例 user:alice）: %q", cr.Subject)
	}
	rel, ok := mapping[cr.Action]
	if !ok {
		return "", false, fmt.Errorf("未知の action: %q", cr.Action)
	}
	if cr.ResourceType == "" || cr.ResourceID == "" {
		return "", false, errors.New("resource_type と resource_id は必須")
	}
	switch cr.Consistency {
	case "", "default":
	case "higher":
		higher = true
	default:
		return "", false, fmt.Errorf("consistency は default か higher: %q", cr.Consistency)
	}
	return rel, higher, nil
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var in CheckRequest
	if err := decode(r, &in); err != nil {
		badRequest(w, r, err)
		return
	}
	rel, higher, err := in.validate(s.mapping)
	if err != nil {
		badRequest(w, r, err)
		return
	}
	allowed, err := s.fga.Check(r.Context(), fga.Tuple{
		Subject: in.Subject, Relation: rel, Object: in.ResourceType + ":" + in.ResourceID,
	}, higher)
	if err != nil {
		upstream(w, r, err)
		return
	}
	writeJSON(w, map[string]bool{"allowed": allowed})
}

const batchLimit = 100

func (s *Server) batchCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Checks []CheckRequest `json:"checks"`
	}
	if err := decode(r, &in); err != nil {
		badRequest(w, r, err)
		return
	}
	if len(in.Checks) == 0 || len(in.Checks) > batchLimit {
		badRequest(w, r, fmt.Errorf("checks は 1〜%d 件", batchLimit))
		return
	}
	type result struct {
		Allowed bool `json:"allowed"`
	}
	results := make([]result, len(in.Checks))
	for i, cr := range in.Checks {
		rel, higher, err := cr.validate(s.mapping)
		if err != nil {
			badRequest(w, r, fmt.Errorf("checks[%d]: %w", i, err))
			return
		}
		allowed, err := s.fga.Check(r.Context(), fga.Tuple{
			Subject: cr.Subject, Relation: rel, Object: cr.ResourceType + ":" + cr.ResourceID,
		}, higher)
		if err != nil {
			upstream(w, r, err)
			return
		}
		results[i] = result{Allowed: allowed}
	}
	writeJSON(w, map[string]any{"results": results})
}

func (s *Server) listObjects(w http.ResponseWriter, r *http.Request) {
	var in CheckRequest // resource_id は使わない
	if err := decode(r, &in); err != nil {
		badRequest(w, r, err)
		return
	}
	in.ResourceID = "-" // validate の必須チェックを通すためのダミー
	rel, higher, err := in.validate(s.mapping)
	if err != nil {
		badRequest(w, r, err)
		return
	}
	objects, err := s.fga.ListObjects(r.Context(), in.Subject, rel, in.ResourceType, higher)
	if err != nil {
		upstream(w, r, err)
		return
	}
	// "photo:123" → "123"。呼び出し側は WHERE IN に使う素の ID が欲しい
	ids := make([]string, 0, len(objects))
	for _, o := range objects {
		ids = append(ids, strings.TrimPrefix(o, in.ResourceType+":"))
	}
	writeJSON(w, map[string]any{"object_ids": ids})
}

// TupleInput は書き込み対象のタプル。relation はモデルの語彙そのもの
// （owner / parent / operator）。action ではない——タプルは関係の宣言であり判定ではない。
type TupleInput struct {
	Subject  string `json:"subject"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

func (t TupleInput) validate() error {
	if !strings.Contains(t.Subject, ":") || !strings.Contains(t.Object, ":") || t.Relation == "" {
		return fmt.Errorf("tuple は subject(type:id) / relation / object(type:id) が必須: %+v", t)
	}
	return nil
}

func (s *Server) tuplesWrite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Writes  []TupleInput `json:"writes"`
		Deletes []TupleInput `json:"deletes"`
	}
	if err := decode(r, &in); err != nil {
		badRequest(w, r, err)
		return
	}
	for i, t := range append(append([]TupleInput{}, in.Writes...), in.Deletes...) {
		if err := t.validate(); err != nil {
			badRequest(w, r, fmt.Errorf("tuples[%d]: %w", i, err))
			return
		}
	}

	// 同一タプルへの重複適用が無害（自然冪等）であることは API の性質として保証する
	// （docs/03-platform.md。Eventual 化で at-least-once になっても受け側で吸収できる）。
	// OpenFGA は既存タプルの write / 不存在タプルの delete をエラーにするため、
	// 先に read で絞ってから書く。絞り込みと書き込みの間に同じタプルが書かれる競合は
	// あり得るが、その場合の失敗も「もう望みの状態になっている」なので1度だけやり直す。
	var written, deleted, skipped int
	for attempt := 0; ; attempt++ {
		var writes, deletes []fga.Tuple
		for _, t := range in.Writes {
			tu := fga.Tuple(t)
			ok, err := s.fga.TupleExists(r.Context(), tu)
			if err != nil {
				upstream(w, r, err)
				return
			}
			if ok {
				skipped++
			} else {
				writes = append(writes, tu)
			}
		}
		for _, t := range in.Deletes {
			tu := fga.Tuple(t)
			ok, err := s.fga.TupleExists(r.Context(), tu)
			if err != nil {
				upstream(w, r, err)
				return
			}
			if !ok {
				skipped++
			} else {
				deletes = append(deletes, tu)
			}
		}
		if len(writes) == 0 && len(deletes) == 0 {
			break
		}
		err := s.fga.Write(r.Context(), writes, deletes)
		if err == nil {
			written += len(writes)
			deleted += len(deletes)
			break
		}
		var apiErr *fga.APIError
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
			// 競合の可能性。絞り直して1度だけやり直す
			skipped = 0
			continue
		}
		upstream(w, r, err)
		return
	}
	writeJSON(w, map[string]int{"written": written, "deleted": deleted, "skipped": skipped})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("リクエスト本文を読めない: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, r *http.Request, err error) {
	problem.Write(w, r, problem.New(http.StatusBadRequest, "invalid_request", err.Error()))
}

func upstream(w http.ResponseWriter, r *http.Request, err error) {
	problem.Write(w, r, problem.New(http.StatusBadGateway, "openfga_unavailable", err.Error()))
}
