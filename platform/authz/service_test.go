package authz

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/rikukaInoue/greenfield/platform/authz/fga"
)

// fakeFGA はメモリ上のタプル集合。Check は完全一致のみ（導出はしない。
// 導出の正しさは実 OpenFGA に対する authz-check.sh が確かめる）。
type fakeFGA struct {
	tuples     map[string]bool
	lastHigher bool
	writeCalls int
}

func newFake() *fakeFGA { return &fakeFGA{tuples: map[string]bool{}} }

func k(t fga.Tuple) string { return t.Subject + "|" + t.Relation + "|" + t.Object }

func (f *fakeFGA) Check(_ context.Context, t fga.Tuple, higher bool) (bool, error) {
	f.lastHigher = higher
	return f.tuples[k(t)], nil
}

func (f *fakeFGA) ListObjects(_ context.Context, user, relation, typ string, _ bool) ([]string, error) {
	var out []string
	for key, ok := range f.tuples {
		if !ok {
			continue
		}
		p := strings.Split(key, "|")
		if p[0] == user && p[1] == relation && strings.HasPrefix(p[2], typ+":") {
			out = append(out, p[2])
		}
	}
	return out, nil
}

func (f *fakeFGA) TupleExists(_ context.Context, t fga.Tuple) (bool, error) {
	return f.tuples[k(t)], nil
}

func (f *fakeFGA) Write(_ context.Context, writes, deletes []fga.Tuple) error {
	f.writeCalls++
	for _, t := range writes {
		f.tuples[k(t)] = true
	}
	for _, t := range deletes {
		delete(f.tuples, k(t))
	}
	return nil
}

func post(t *testing.T, s *Server, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var out map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("応答が JSON でない: %v: %s", err, w.Body.String())
		}
	}
	return w.Code, out
}

func TestCheckMapsActionToRelation(t *testing.T) {
	f := newFake()
	f.tuples["user:alice|viewer|photo:1"] = true
	s := NewServer(f, DefaultMapping)

	code, out := post(t, s, "/check", `{"subject":"user:alice","action":"photo.view","resource_type":"photo","resource_id":"1"}`)
	if code != 200 || out["allowed"] != true {
		t.Fatalf("photo.view → viewer が引けていない: %d %v", code, out)
	}
	// photo.publish は editor へ写る
	f.tuples["user:alice|editor|photo:1"] = true
	code, out = post(t, s, "/check", `{"subject":"user:alice","action":"photo.publish","resource_type":"photo","resource_id":"1"}`)
	if code != 200 || out["allowed"] != true {
		t.Fatalf("photo.publish → editor が引けていない: %d %v", code, out)
	}
}

func TestCheckRejectsUnknownActionAndBadSubject(t *testing.T) {
	s := NewServer(newFake(), DefaultMapping)
	if code, _ := post(t, s, "/check", `{"subject":"user:alice","action":"photo.destroy","resource_type":"photo","resource_id":"1"}`); code != 400 {
		t.Fatalf("未知 action が %d", code)
	}
	if code, _ := post(t, s, "/check", `{"subject":"alice","action":"photo.view","resource_type":"photo","resource_id":"1"}`); code != 400 {
		t.Fatalf("type:id でない subject が %d", code)
	}
	if code, _ := post(t, s, "/check", `{"subject":"user:alice","action":"photo.view","resource_type":"photo","resource_id":"1","consistency":"strong"}`); code != 400 {
		t.Fatalf("未知 consistency が %d", code)
	}
}

func TestCheckPassesConsistencyHint(t *testing.T) {
	f := newFake()
	s := NewServer(f, DefaultMapping)
	post(t, s, "/check", `{"subject":"user:a","action":"photo.view","resource_type":"photo","resource_id":"1","consistency":"higher"}`)
	if !f.lastHigher {
		t.Fatal("consistency=higher が OpenFGA へ透過されていない")
	}
	post(t, s, "/check", `{"subject":"user:a","action":"photo.view","resource_type":"photo","resource_id":"1"}`)
	if f.lastHigher {
		t.Fatal("既定なのに HIGHER_CONSISTENCY が付いている")
	}
}

func TestBatchCheckKeepsOrder(t *testing.T) {
	f := newFake()
	f.tuples["user:alice|viewer|photo:1"] = true
	s := NewServer(f, DefaultMapping)
	code, out := post(t, s, "/batch-check", `{"checks":[
	  {"subject":"user:alice","action":"photo.view","resource_type":"photo","resource_id":"2"},
	  {"subject":"user:alice","action":"photo.view","resource_type":"photo","resource_id":"1"}
	]}`)
	if code != 200 {
		t.Fatalf("batch-check: %d %v", code, out)
	}
	results := out["results"].([]any)
	got := []bool{results[0].(map[string]any)["allowed"].(bool), results[1].(map[string]any)["allowed"].(bool)}
	if !reflect.DeepEqual(got, []bool{false, true}) {
		t.Fatalf("順序が保存されていない: %v", got)
	}
}

func TestListObjectsStripsTypePrefix(t *testing.T) {
	f := newFake()
	f.tuples["user:alice|viewer|photo:1"] = true
	f.tuples["user:alice|viewer|photo:7"] = true
	f.tuples["user:bob|viewer|photo:2"] = true
	s := NewServer(f, DefaultMapping)
	code, out := post(t, s, "/list-objects", `{"subject":"user:alice","action":"photo.view","resource_type":"photo"}`)
	if code != 200 {
		t.Fatalf("list-objects: %d %v", code, out)
	}
	var ids []string
	for _, v := range out["object_ids"].([]any) {
		ids = append(ids, v.(string))
	}
	if len(ids) != 2 || strings.Contains(ids[0], ":") {
		t.Fatalf("素の ID が返っていない: %v", ids)
	}
}

func TestTuplesWriteIsNaturallyIdempotent(t *testing.T) {
	f := newFake()
	s := NewServer(f, DefaultMapping)
	body := `{"writes":[{"subject":"user:alice","relation":"owner","object":"photo:1"}]}`

	code, out := post(t, s, "/tuples:write", body)
	if code != 200 || out["written"].(float64) != 1 {
		t.Fatalf("1回目: %d %v", code, out)
	}
	code, out = post(t, s, "/tuples:write", body)
	if code != 200 || out["written"].(float64) != 0 || out["skipped"].(float64) != 1 {
		t.Fatalf("2回目が冪等でない: %d %v", code, out)
	}
	if f.writeCalls != 1 {
		t.Fatalf("OpenFGA への write が %d 回（重複適用が素通りしている）", f.writeCalls)
	}

	// 不存在タプルの delete も無害
	code, out = post(t, s, "/tuples:write", `{"deletes":[{"subject":"user:zed","relation":"owner","object":"photo:9"}]}`)
	if code != 200 || out["deleted"].(float64) != 0 || out["skipped"].(float64) != 1 {
		t.Fatalf("不存在 delete が無害でない: %d %v", code, out)
	}
}

func TestModelIsValidJSON(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal([]byte(Model), &v); err != nil {
		t.Fatalf("Model が JSON として壊れている: %v", err)
	}
	if v["schema_version"] != "1.1" {
		t.Fatalf("schema_version: %v", v["schema_version"])
	}
}
