// genapi は各サービスの huma API から OpenAPI スペックを生成し api/<service>/ へ書く。
// メジャー1は <listener>.openapi.json、メジャー n（2 以上）は <listener>.v<n>.openapi.json（docs/adr/0017）。
//
//	go run ./dev/genapi            # 生成物を書き出す
//	go run ./dev/genapi -check     # 生成物とリポジトリの内容が一致するか検証する（CI）
//
// スペックの源泉は常に huma の型であり、api/ は手書きしない（conventions/api-design.md §3.1）。
// api/ はコンテキスト間で共有してよい唯一の契約置き場であり、差分は PR レビューの対象になる。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/httpapi"
	gear "github.com/rikukaInoue/greenfield/services/gear/app"
	photo "github.com/rikukaInoue/greenfield/services/photo/app"
	// scaffold:genapi-imports
)

// specs はサービス名 → リスナー別 API。新サービスは scaffold がここへ登録する。
func specs() map[string]map[httpapi.Listener]httpapi.API {
	return map[string]map[httpapi.Listener]httpapi.API{
		"photo": photo.APIs(nil),
		"gear":  gear.APIs(nil),
		// scaffold:specs
	}
}

func main() {
	check := flag.Bool("check", false, "生成物と一致するか検証する（書き換えない）")
	flag.Parse()

	root, err := findRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "genapi:", err)
		os.Exit(1)
	}

	var stale []string
	for service, apis := range specs() {
		generated := map[string]bool{}
		for _, l := range httpapi.Listeners {
			api, ok := apis[l]
			if !ok {
				continue
			}
			for major, h := range api.Majors() {
				name := specFile(l, major)
				generated[name] = true
				want, err := marshal(h)
				if err != nil {
					fmt.Fprintf(os.Stderr, "genapi: %s/%s: %v\n", service, name, err)
					os.Exit(1)
				}
				rel := filepath.Join("api", service, name)
				path := filepath.Join(root, rel)
				if *check {
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, want) {
						stale = append(stale, rel)
					}
					continue
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					fmt.Fprintln(os.Stderr, "genapi:", err)
					os.Exit(1)
				}
				if err := os.WriteFile(path, want, 0o644); err != nil {
					fmt.Fprintln(os.Stderr, "genapi:", err)
					os.Exit(1)
				}
				fmt.Printf("wrote %s\n", rel)
			}
		}
		// コードから消えたメジャーのスペックが api/ に残っていないか（アダプタを消してもファイルだけ残る）
		existing, _ := filepath.Glob(filepath.Join(root, "api", service, "*.openapi.json"))
		for _, p := range existing {
			if !generated[filepath.Base(p)] {
				rel, _ := filepath.Rel(root, p)
				if *check {
					stale = append(stale, rel+"（コードに無い）")
					continue
				}
				if err := os.Remove(p); err != nil {
					fmt.Fprintln(os.Stderr, "genapi:", err)
					os.Exit(1)
				}
				fmt.Printf("removed %s\n", rel)
			}
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "genapi: スペックが古い: %v\n  再生成: mise run api\n", stale)
		os.Exit(1)
	}
	if *check {
		fmt.Println("genapi: すべてのスペックが最新")
	}
}

// specFile はメジャーに対応するスペックのファイル名を返す。
func specFile(l httpapi.Listener, major int) string {
	if major == 1 {
		return string(l) + ".openapi.json"
	}
	return fmt.Sprintf("%s.v%d.openapi.json", l, major)
}

// marshal は安定した（キー順が固定された）整形JSONを返す。差分レビューのため。
func marshal(api huma.API) ([]byte, error) {
	raw, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.work が見つからない")
		}
		dir = parent
	}
}
