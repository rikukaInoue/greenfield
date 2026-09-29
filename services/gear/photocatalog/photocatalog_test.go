package photocatalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rikukaInoue/greenfield/core/httpclient"
)

// stub は photo internal の代役。受けたヘッダを記録し、固定の作例を返す。
func stub(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	var got http.Header
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.URL.Path != "/gear-items/5/photos" {
			t.Errorf("path: %s", r.URL.Path)
		}
		url := "https://img.example/1"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"photos": []map[string]any{
				{"id": 1, "caption": "作例", "image_url": url, "owner_id": "u", "created_at": "2026-01-01T00:00:00Z"},
				{"id": 2, "caption": "URLなし", "owner_id": "u", "created_at": "2026-01-01T00:00:00Z"},
			},
		})
	}))
	t.Cleanup(s.Close)
	return s, &got
}

func TestConvertsGeneratedTypesAndAttachesToken(t *testing.T) {
	s, got := stub(t)
	cat, err := New(s.URL, httpclient.Client(httpclient.StaticTokenSource("tok-123")))
	if err != nil {
		t.Fatal(err)
	}
	refs, err := cat.PublicPhotosByItem(context.Background(), 5, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("Authorization") != "Bearer tok-123" {
		t.Fatalf("M2M トークンが付いていない: %q", got.Get("Authorization"))
	}
	if got.Get("Idempotency-Key") != "" {
		t.Fatal("読み取りに冪等キーが付いている（変更系のみのはず）")
	}
	if len(refs) != 2 || refs[0].ImageURL != "https://img.example/1" || refs[1].ImageURL != "" {
		t.Fatalf("型変換が壊れている: %+v", refs)
	}
}

func TestNon200IsError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(s.Close)
	cat, _ := New(s.URL, httpclient.Client(httpclient.StaticTokenSource("t")))
	if _, err := cat.PublicPhotosByItem(context.Background(), 5, 12); err == nil {
		t.Fatal("403 が素通り")
	}
}
