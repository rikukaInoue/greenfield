// scaffold は新サービスの骨格を生成する。
//
//	go run ./dev/scaffold new-service <name>
//
// 生成物: services/<name>/（モジュール、cmd、app、handler×3、各層のディレクトリ）と
// services/<name>-client/（生成クライアント置き場）。あわせて go.work の use、
// dev/go.mod の require/replace、dev/allinone/services.go の登録行（ポートは既存の最大 +10）を更新する。
package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed all:templates
var templates embed.FS

const modulePrefix = "github.com/rikukaInoue/greenfield"

type data struct {
	Name     string // サービス名（ディレクトリ・バイナリ名）
	Pkg      string // Goパッケージ名（allinoneのimport alias）
	Env      string // 環境変数プレフィックス
	Module   string // モジュールパス
	Base     int    // externalポート
	Internal int
	Admin    int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "scaffold:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 || args[0] != "new-service" {
		return errors.New("usage: scaffold new-service <name>")
	}
	name := args[1]
	if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(name) {
		return fmt.Errorf("invalid service name %q: use [a-z][a-z0-9]*", name)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "services", name)); err == nil {
		return fmt.Errorf("services/%s already exists", name)
	}

	registry := filepath.Join(root, "dev", "allinone", "services.go")
	base, err := nextBase(registry)
	if err != nil {
		return err
	}
	d := data{
		Name: name, Pkg: name, Env: strings.ToUpper(name),
		Module: modulePrefix + "/services/" + name,
		Base:   base, Internal: base + 1, Admin: base + 2,
	}

	if err := render(root, d); err != nil {
		return err
	}
	if err := insertBefore(filepath.Join(root, "go.work"), "\t// scaffold:use",
		fmt.Sprintf("\t./services/%s\n\t./services/%s-client\n", name, name)); err != nil {
		return err
	}
	if err := insertBefore(registry, "\t// scaffold:imports",
		fmt.Sprintf("\t%s \"%s/app\"\n", d.Pkg, d.Module)); err != nil {
		return err
	}
	if err := insertBefore(registry, "\t// scaffold:services",
		fmt.Sprintf("\t{Name: %q, Base: %d, run: func(ctx context.Context) error {\n\t\te, i, a := addrs(%d)\n\t\treturn %s.Run(ctx, %s.Config{ExternalAddr: e, InternalAddr: i, AdminAddr: a})\n\t}},\n",
			name, base, base, d.Pkg, d.Pkg)); err != nil {
		return err
	}
	dev := filepath.Join(root, "dev")
	for _, a := range [][]string{
		{"mod", "edit", "-require=" + d.Module + "@v0.0.0"},
		{"mod", "edit", "-replace=" + d.Module + "=../services/" + name},
	} {
		cmd := exec.Command("go", a...)
		cmd.Dir, cmd.Stderr = dev, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go %s: %w", strings.Join(a, " "), err)
		}
	}
	fmt.Printf("generated services/%s (ports %d/%d/%d) and services/%s-client\n", name, d.Base, d.Internal, d.Admin, name)
	return nil
}

// findRoot はcwdから上へ辿り go.work のあるディレクトリを返す。
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
			return "", errors.New("go.work not found in any parent directory")
		}
		dir = parent
	}
}

// nextBase は登録済みの最大externalポート +10 を返す。未登録なら8080。
func nextBase(registry string) (int, error) {
	b, err := os.ReadFile(registry)
	if err != nil {
		return 0, err
	}
	max := 8070
	for _, m := range regexp.MustCompile(`Base: (\d+)`).FindAllStringSubmatch(string(b), -1) {
		n, _ := strconv.Atoi(m[1])
		if n > max {
			max = n
		}
	}
	return max + 10, nil
}

// render はtemplates/ 配下を services/<name>/ 等へ展開する。
// パス中の "__name__" は置換し、拡張子 .tmpl は外す。
func render(root string, d data) error {
	return fs.WalkDir(templates, "templates", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "templates/")
		rel = strings.ReplaceAll(rel, "__name__", d.Name)
		rel = strings.TrimSuffix(rel, ".tmpl")
		dst := filepath.Join(root, rel)

		src, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		t, err := template.New(p).Parse(string(src))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		return t.Execute(f, d)
	})
}

// insertBefore はmarker行の直前にtextを挿入する。
func insertBefore(path, marker, text string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	i := strings.Index(s, marker)
	if i < 0 {
		return fmt.Errorf("%s: marker %q not found", path, strings.TrimSpace(marker))
	}
	return os.WriteFile(path, []byte(s[:i]+text+s[i:]), 0o644)
}
