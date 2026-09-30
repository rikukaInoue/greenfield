// driftcheck は go.work 配下の全 go.mod を読み、**2つ以上のモジュールが require する
// 共有依存**のバージョン乖離（ドリフト）を報告する(#205)。
//
//	go run ./dev/driftcheck            # 乖離があれば表を出す。無ければ「ok: N 依存が収斂」
//	go run ./dev/driftcheck -strict    # 乖離があれば exit 1（将来 CI を fail にする時用）
//
// internal-01 は「共有系依存のバージョンドリフトは四半期ごとに収斂させる」と定めるが、
// 宣言だけでは観測できない。fk:check や outbox の pending 監視と同じ発想で、
// 収斂の対象がいま何件あるかを機械が数える。既定は警告のみ（exit 0）:
// fail にすると依存更新の PR が構造的に片側から通せなくなるため、締めるのは運用してから。
//
// 自リポジトリ配下（replace されるモジュール間参照）は対象外。あれは replace で
// ローカル解決され、バージョンの意味を持たない（lint:replaces の管轄）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

const selfPrefix = "github.com/rikukaInoue/greenfield"

type req struct {
	module  string // 依存を要求しているモジュール（例: ./core）
	version string
}

func main() {
	strict := flag.Bool("strict", false, "乖離があれば exit 1 にする")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	mods, err := workModules(filepath.Join(root, "go.work"))
	if err != nil {
		fatal(err)
	}
	if len(mods) < 3 {
		fatal(fmt.Errorf("go.work のモジュールが %d 個しかない。検査の前提が崩れている", len(mods)))
	}

	// 依存 → それを要求するモジュールと版
	deps := map[string][]req{}
	for _, m := range mods {
		data, err := os.ReadFile(filepath.Join(root, m, "go.mod"))
		if err != nil {
			fatal(err)
		}
		f, err := modfile.Parse(m+"/go.mod", data, nil)
		if err != nil {
			fatal(err)
		}
		for _, r := range f.Require {
			if strings.HasPrefix(r.Mod.Path, selfPrefix) {
				continue // 自リポジトリ間は replace 解決（lint:replaces の管轄）
			}
			// indirect も対象: ビルドに使われる版はどちらでも同じで、乖離の実害も同じ
			deps[r.Mod.Path] = append(deps[r.Mod.Path], req{module: m, version: r.Mod.Version})
		}
	}

	shared, drifted := 0, 0
	var report []string
	for _, dep := range sortedKeys(deps) {
		rs := deps[dep]
		if len(rs) < 2 {
			continue
		}
		shared++
		versions := map[string][]string{}
		for _, r := range rs {
			versions[r.version] = append(versions[r.version], r.module)
		}
		if len(versions) == 1 {
			continue
		}
		drifted++
		var lines []string
		for _, v := range sortedKeys(versions) {
			lines = append(lines, fmt.Sprintf("      %-16s %s", v, strings.Join(versions[v], " ")))
		}
		report = append(report, fmt.Sprintf("  %s（%d 版）:\n%s", dep, len(versions), strings.Join(lines, "\n")))
	}

	fmt.Printf("走査対象: モジュール %d 個 / 共有依存（2モジュール以上が require） %d 件\n", len(mods), shared)
	if drifted == 0 {
		fmt.Printf("ok: 共有依存 %d 件はすべて収斂している\n", shared)
		return
	}
	fmt.Printf("バージョン乖離 %d 件:\n%s\n", drifted, strings.Join(report, "\n"))
	fmt.Println("収斂のさせ方: 低い側のモジュールで `go get <dep>@<高い側の版> && go mod tidy`")
	if *strict {
		os.Exit(1)
	}
}

func workModules(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := modfile.ParseWork(path, data, nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, u := range f.Use {
		out = append(out, u.Path)
	}
	sort.Strings(out)
	return out, nil
}

func repoRoot() (string, error) {
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

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "driftcheck:", err)
	os.Exit(1)
}
