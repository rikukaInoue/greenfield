package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fixture は core ← photo, gear と、gear が photo-client を使う（client 再生成が利用側へ波及する）構成を作る。
func fixture(t *testing.T) *repo {
	t.Helper()
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.work", "go 1.26.0\n\nuse (\n\t./core\n\t./dev\n\t./services/photo\n\t./services/photo-client\n\t./services/gear\n\t./services/gear-client\n\t./platform/authz\n)\n")
	write("core/go.mod", "module example.com/core\n")
	write("services/photo-client/go.mod", "module example.com/services/photo-client\n")
	write("services/gear-client/go.mod", "module example.com/services/gear-client\n")
	write("services/photo/go.mod", "module example.com/services/photo\n\nreplace example.com/core => ../../core\n")
	write("services/gear/go.mod", "module example.com/services/gear\n\nreplace example.com/core => ../../core\n\nreplace example.com/services/photo-client => ../photo-client\n")
	write("dev/go.mod", "module example.com/dev\n\nreplace example.com/core => ../core\n\nreplace example.com/services/photo => ../services/photo\n\nreplace example.com/services/gear => ../services/gear\n")
	write("platform/authz/go.mod", "module example.com/platform/authz\n\nreplace example.com/core => ../../core\n")
	write("frontend/packages/photo-api/package.json", "{}")
	r, err := loadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAffected(t *testing.T) {
	r := fixture(t)
	all := []string{"./core", "./dev", "./services/photo", "./services/photo-client", "./services/gear", "./services/gear-client", "./platform/authz"}

	cases := []struct {
		name     string
		changed  []string
		wantGo   []string
		frontend bool
		keycloak bool
		all      bool
	}{
		// #22: photo だけの変更で gear は走らない
		{"photo のみ", []string{"services/photo/usecase/photo.go"}, []string{"./dev", "./services/photo"}, true, false, false},
		{"gear のみ", []string{"services/gear/app/app.go"}, []string{"./dev", "./services/gear"}, false, false, false},
		// #23: core の変更は依存する全モジュールへ広がる
		{"core", []string{"core/flags/flags.go"}, []string{"./core", "./dev", "./services/photo", "./services/gear", "./platform/authz"}, true, false, false},
		// #23: client の再生成は利用側（gear）へ広がる
		{"photo-client の再生成", []string{"services/photo-client/client.gen.go"}, []string{"./services/photo-client", "./services/gear", "./dev"}, false, false, false},
		{"frontend のみ", []string{"frontend/apps/web/app/root.tsx"}, []string{}, true, false, false},
		{"API 契約", []string{"api/photo/external.openapi.json"}, []string{"./dev", "./services/photo"}, true, false, false},
		{"ドキュメントのみ", []string{"docs/verification-log.md", "services/photo/migrations/README.md"}, []string{}, false, false, false},
		{"変更なし", nil, []string{}, false, false, false},
		// 検査ツールと設定は全部に効く
		{"dev のツール", []string{"dev/scripts/schema-dump.sh"}, all, true, true, true},
		{"mise.toml", []string{"mise.toml"}, all, true, true, true},
		{"CI 定義", []string{".github/workflows/ci.yml"}, all, true, true, true},
		{"未知のサービスの api/", []string{"api/unknown/external.openapi.json"}, all, true, true, true},
		// realm-as-code: realm.json の差分は認証基盤の設定変更。keycloak だけを起こす
		{"realm 定義", []string{"deploy/compose/keycloak/realm.json"}, []string{}, false, true, false},
		{"keycloak の検証スクリプト", []string{"dev/scripts/keycloak-claims.sh"}, []string{}, false, true, false},
		// authz の検証スクリプトは platform/authz だけを起こす（全モジュールへ広げない）
		{"authz の検証スクリプト", []string{"dev/scripts/authz-check.sh"}, []string{"./platform/authz"}, false, false, false},
		{"platform/authz のコード", []string{"platform/authz/service.go"}, []string{"./platform/authz"}, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.affected(tc.changed)
			want := order(r, tc.wantGo)
			if !reflect.DeepEqual(got.Go, want) {
				t.Errorf("Go = %v, want %v\nreasons: %v", got.Go, want, got.Reasons)
			}
			if got.Frontend != tc.frontend {
				t.Errorf("Frontend = %v, want %v", got.Frontend, tc.frontend)
			}
			if got.Keycloak != tc.keycloak {
				t.Errorf("Keycloak = %v, want %v\nreasons: %v", got.Keycloak, tc.keycloak, got.Reasons)
			}
			if got.All != tc.all {
				t.Errorf("All = %v, want %v", got.All, tc.all)
			}
			if len(got.Reasons) == 0 {
				t.Error("理由が空（なぜ走る/走らないかを必ず出す）")
			}
		})
	}
}

func TestLoadRepoRejectsReplaceOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26.0\n\nuse ./a\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "a"), 0o755)
	os.WriteFile(filepath.Join(root, "a", "go.mod"), []byte("module example.com/a\n\nreplace example.com/b => ../b\n"), 0o644)
	if _, err := loadRepo(root); err == nil {
		t.Fatal("go.work に無い replace 先を黙って無視した")
	}
}

// order は want を go.work の順に並べる。
func order(r *repo, want []string) []string {
	set := map[string]bool{}
	for _, w := range want {
		set[w] = true
	}
	out := []string{}
	for _, m := range r.modules {
		if set[m] {
			out = append(out, m)
		}
	}
	return out
}
