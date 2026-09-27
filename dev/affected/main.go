// affected は変更されたファイルから、CI で検査すべきモジュールを求める。
//
//	go run ./dev/affected -base origin/main                          # JSON を標準出力へ
//	go run ./dev/affected -base origin/main -github-output "$GITHUB_OUTPUT"
//	go run ./dev/affected -all -github-output "$GITHUB_OUTPUT"       # 全部（main への push 等）
//
// 依存グラフは各モジュールの go.mod の replace（相対パス）から作る。手書きの対応表は持たない。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

// Result は CI へ渡す判定結果。
type Result struct {
	All      bool     `json:"all"`
	Go       []string `json:"go"`
	Frontend bool     `json:"frontend"`
	Reasons  []string `json:"reasons"`
}

type repo struct {
	modules []string            // go.work の use（./core 形式）
	deps    map[string][]string // モジュール → replace で参照するモジュール
	// frontendServices は frontend/packages/<name>-api がある（frontend が API を使う）サービス。
	frontendServices map[string]bool
}

func main() {
	var (
		base   = flag.String("base", "", "比較元の ref（例: origin/main）。merge-base からの差分を見る")
		all    = flag.Bool("all", false, "差分を見ずに全部を対象にする")
		output = flag.String("github-output", "", "GitHub Actions の出力ファイル（$GITHUB_OUTPUT）")
	)
	flag.Parse()
	if err := run(*base, *all, *output); err != nil {
		fmt.Fprintln(os.Stderr, "affected:", err)
		os.Exit(1)
	}
}

func run(base string, all bool, output string) error {
	root, err := gitRoot()
	if err != nil {
		return err
	}
	r, err := loadRepo(root)
	if err != nil {
		return err
	}
	var res Result
	switch {
	case all:
		res = r.everything("-all 指定")
	case base == "":
		return errors.New("-base か -all のどちらかが必要")
	default:
		changed, err := changedFiles(root, base)
		if err != nil {
			return err
		}
		res = r.affected(changed)
	}
	for _, reason := range res.Reasons {
		fmt.Fprintln(os.Stderr, reason)
	}
	fmt.Fprintf(os.Stderr, "対象: Go %d / %d モジュール, frontend=%t\n", len(res.Go), len(r.modules), res.Frontend)

	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	if output == "" {
		return nil
	}
	goJSON, err := json.Marshal(res.Go)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(output, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "go=%s\nfrontend=%t\nall=%t\n", goJSON, res.Frontend, res.All)
	return err
}

func gitRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// changedFiles は base との merge-base から HEAD までに変わったファイルを返す。
// 名前変更は旧パスも対象にするため --no-renames で削除 + 追加として受け取る。
func changedFiles(root, base string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--no-renames", base+"...HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s...HEAD: %w", base, err)
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func loadRepo(root string) (*repo, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		return nil, err
	}
	work, err := modfile.ParseWork("go.work", b, nil)
	if err != nil {
		return nil, err
	}
	r := &repo{deps: map[string][]string{}, frontendServices: map[string]bool{}}
	for _, u := range work.Use {
		r.modules = append(r.modules, "./"+filepath.ToSlash(filepath.Clean(u.Path)))
	}
	known := map[string]bool{}
	for _, m := range r.modules {
		known[m] = true
	}
	for _, m := range r.modules {
		b, err := os.ReadFile(filepath.Join(root, m, "go.mod"))
		if err != nil {
			return nil, err
		}
		mf, err := modfile.Parse(m+"/go.mod", b, nil)
		if err != nil {
			return nil, err
		}
		for _, rep := range mf.Replace {
			if !strings.HasPrefix(rep.New.Path, ".") {
				continue
			}
			dep := "./" + filepath.ToSlash(filepath.Clean(filepath.Join(m, rep.New.Path)))
			if !known[dep] {
				return nil, fmt.Errorf("%s/go.mod: replace 先 %s が go.work に無い", m, dep)
			}
			r.deps[m] = append(r.deps[m], dep)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "frontend", "packages"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), "-api"); ok && e.IsDir() {
			r.frontendServices[name] = true
		}
	}
	return r, nil
}

// affected は変更ファイルから対象を求める。モジュールに属さない設定・ツールの変更は全部を対象にする。
func (r *repo) affected(changed []string) Result {
	direct := map[string][]string{} // モジュール → 理由
	frontend := []string{}
	var global []string
	for _, f := range changed {
		switch {
		case ignorable(f):
			continue
		case strings.HasPrefix(f, "frontend/"):
			frontend = append(frontend, f)
			continue
		case strings.HasPrefix(f, "api/"):
			svc := strings.SplitN(strings.TrimPrefix(f, "api/"), "/", 2)[0]
			m := "./services/" + svc
			if r.has(m) {
				direct[m] = append(direct[m], f)
			} else {
				global = append(global, f)
			}
			continue
		}
		owner := r.owner(f)
		switch owner {
		case "", "./dev":
			// dev は全モジュールの検査ツール（schema-dump / querylint / genapi 等）なので全部に効く
			global = append(global, f)
		default:
			direct[owner] = append(direct[owner], f)
		}
	}
	if len(global) > 0 {
		return r.everything(fmt.Sprintf("全体に効く変更: %s", strings.Join(limit(global, 5), ", ")))
	}

	res := Result{}
	set := map[string]bool{}
	for m, files := range direct {
		res.Reasons = append(res.Reasons, fmt.Sprintf("%s: 変更 %s", m, strings.Join(limit(files, 3), ", ")))
		for _, d := range r.dependents(m) {
			if !set[d] {
				set[d] = true
				if d != m {
					res.Reasons = append(res.Reasons, fmt.Sprintf("%s: %s に依存", d, m))
				}
			}
		}
	}
	for _, m := range r.modules {
		if set[m] {
			res.Go = append(res.Go, m)
		}
	}
	if len(frontend) > 0 {
		res.Frontend = true
		res.Reasons = append(res.Reasons, fmt.Sprintf("frontend: 変更 %s", strings.Join(limit(frontend, 3), ", ")))
	}
	for _, m := range res.Go {
		if svc, ok := strings.CutPrefix(m, "./services/"); ok && r.frontendServices[svc] {
			res.Frontend = true
			res.Reasons = append(res.Reasons, fmt.Sprintf("frontend: %s の API を使う", m))
		}
	}
	if res.Go == nil {
		res.Go = []string{}
	}
	if len(res.Reasons) == 0 {
		res.Reasons = []string{"検査対象の変更なし（ドキュメントのみ等）"}
	}
	sort.Strings(res.Reasons)
	return res
}

func (r *repo) everything(reason string) Result {
	return Result{All: true, Go: append([]string{}, r.modules...), Frontend: true, Reasons: []string{reason}}
}

func (r *repo) has(m string) bool {
	for _, x := range r.modules {
		if x == m {
			return true
		}
	}
	return false
}

// owner は f を含むモジュールのうち最も深いものを返す。どれにも属さなければ空。
func (r *repo) owner(f string) string {
	best := ""
	for _, m := range r.modules {
		dir := strings.TrimPrefix(m, "./") + "/"
		if strings.HasPrefix(f, dir) && len(m) > len(best) {
			best = m
		}
	}
	return best
}

// dependents は m 自身と、m に（推移的に）依存するモジュールを返す。
func (r *repo) dependents(m string) []string {
	rev := map[string][]string{}
	for from, tos := range r.deps {
		for _, to := range tos {
			rev[to] = append(rev[to], from)
		}
	}
	seen := map[string]bool{m: true}
	queue := []string{m}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range rev[cur] {
			if !seen[d] {
				seen[d] = true
				queue = append(queue, d)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ignorable は CI の対象にならない変更（ドキュメント）か。
func ignorable(f string) bool {
	return strings.HasPrefix(f, "docs/") || strings.HasSuffix(f, ".md")
}

func limit(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(append([]string{}, s[:n]...), fmt.Sprintf("ほか%d件", len(s)-n))
}
