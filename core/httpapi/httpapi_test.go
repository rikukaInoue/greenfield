package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/httpapi"
)

type v1Out struct {
	Body struct {
		Caption string `json:"caption"`
	}
}

type v2Out struct {
	Status int
	Body   struct {
		Title string `json:"title"`
	}
}

func TestAddMajorKeepsSpecsApart(t *testing.T) {
	api := httpapi.New(httpapi.External, httpapi.Options{Service: "svc", Version: "1.2.0"})
	huma.Get(api.Huma, "/things", func(context.Context, *struct{}) (*v1Out, error) {
		out := &v1Out{}
		out.Body.Caption = "v1"
		return out, nil
	})
	v2 := api.AddMajor(2, "2.0.0")
	huma.Get(v2, "/things", func(context.Context, *struct{}) (*v2Out, error) {
		out := &v2Out{Status: http.StatusCreated}
		out.Body.Title = "v2"
		return out, nil
	})

	majors := api.Majors()
	if len(majors) != 2 {
		t.Fatalf("Majors = %d, want 2", len(majors))
	}
	spec1, spec2 := majors[1].OpenAPI(), majors[2].OpenAPI()
	if spec1.Info.Version != "1.2.0" || spec2.Info.Version != "2.0.0" {
		t.Fatalf("version = %s / %s", spec1.Info.Version, spec2.Info.Version)
	}
	if _, ok := spec1.Paths["/v2/things"]; ok {
		t.Error("v1 のスペックに v2 のパスが混ざった")
	}
	if _, ok := spec2.Paths["/v2/things"]; !ok {
		t.Errorf("v2 のパスに接頭辞が無い: %v", keys(spec2.Paths))
	}
	if _, ok := spec2.Paths["/things"]; ok {
		t.Error("v2 のスペックに v1 のパスが混ざった")
	}
	for name := range spec1.Components.Schemas.Map() {
		if strings.Contains(strings.ToLower(name), "v2") {
			t.Errorf("v1 のスペックに v2 のスキーマ %s が混ざった", name)
		}
	}

	// 同じハンドラ（同じリスナー）で両方に届く
	for path, want := range map[string]string{"/things": `"caption":"v1"`, "/v2/things": `"title":"v2"`} {
		rec := httptest.NewRecorder()
		api.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestAddMajorRejectsMismatchedVersion(t *testing.T) {
	api := httpapi.New(httpapi.External, httpapi.Options{Service: "svc", Version: "1.0.0"})
	defer func() {
		if recover() == nil {
			t.Fatal("メジャー 2 に 3.0.0 を渡して通った")
		}
	}()
	api.AddMajor(2, "3.0.0")
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
