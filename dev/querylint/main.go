// querylint は sqlc のクエリファイルが自ドメインのテーブルだけを参照していることを検証する。
//
//	go run ./dev/querylint services/photo [services/gear ...]
//
// 許可リストは services/<name>/db/schema.sql の CREATE TABLE から作る（手書きの許可リストを持たない）。
// 検出するもの: FROM / JOIN / INTO / UPDATE の直後（テーブル位置）に現れる名前のうち、
// (1) `db.table` 形式の修飾参照（他 database への越境は形が違うので機械的に弾く）、(2) 許可リストにないテーブル名。
// テーブル位置に限定するのは、`p.id` のような「別名.カラム」を越境と誤認しないため。
// sqlc 自身も未知のテーブルで生成に失敗するが、こちらは生成器に依存しない第2のゲートとして CI に置く。
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	createTable = regexp.MustCompile("(?i)CREATE TABLE `?([A-Za-z0-9_]+)`?")
	// テーブル位置の名前。`db`.`table` / db.table / table のいずれも拾う
	tableRef    = regexp.MustCompile("(?i)\\b(?:FROM|JOIN|INTO|UPDATE)\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?(?:\\.`?([A-Za-z_][A-Za-z0-9_]*)`?)?")
	lineComment = regexp.MustCompile("(?m)--.*$")
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: querylint <service-dir>...")
		os.Exit(2)
	}
	failed := false
	for _, dir := range os.Args[1:] {
		problems, err := lint(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "querylint: %s: %v\n", dir, err)
			os.Exit(2)
		}
		for _, p := range problems {
			fmt.Println(p)
		}
		if len(problems) > 0 {
			failed = true
		} else {
			fmt.Printf("querylint: %s ok\n", dir)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func lint(dir string) ([]string, error) {
	schema, err := os.ReadFile(filepath.Join(dir, "db", "schema.sql"))
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, m := range createTable.FindAllStringSubmatch(string(schema), -1) {
		allowed[strings.ToLower(m[1])] = true
	}
	// db/queries/ 配下を再帰的に（repository/ と readmodel/ に分かれている）
	var files []string
	err = filepath.WalkDir(filepath.Join(dir, "db", "queries"), func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".sql") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		src := lineComment.ReplaceAllString(string(b), "")
		seen := map[string]bool{}
		for _, m := range tableRef.FindAllStringSubmatch(src, -1) {
			if m[2] != "" {
				ref := m[1] + "." + m[2]
				if !seen[ref] {
					seen[ref] = true
					problems = append(problems, fmt.Sprintf("%s: qualified table %q (cross-database access is forbidden)", f, ref))
				}
				continue
			}
			t := strings.ToLower(m[1])
			if !allowed[t] && !seen["table:"+t] {
				seen["table:"+t] = true
				problems = append(problems, fmt.Sprintf("%s: table %q is not in %s/db/schema.sql", f, m[1], dir))
			}
		}
	}
	sort.Strings(problems)
	return problems, nil
}
