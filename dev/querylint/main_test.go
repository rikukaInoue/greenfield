package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, queries string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "db", "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	schema := "CREATE TABLE `photos` (`id` bigint, `gear_item_id` bigint);\nCREATE TABLE `outbox` (`id` bigint);\n"
	if err := os.WriteFile(filepath.Join(dir, "db", "schema.sql"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db", "queries", "q.sql"), []byte(queries), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLint(t *testing.T) {
	cases := []struct {
		name    string
		queries string
		want    []string // 問題メッセージに含まれるべき部分文字列。空なら問題なし
	}{
		{"own tables", "SELECT p.id FROM photos p JOIN outbox o ON o.id = p.id WHERE p.id IN (sqlc.slice('ids'));", nil},
		{"insert/update", "INSERT INTO photos (id) VALUES (?);\nUPDATE photos SET id = ? WHERE id = ?;", nil},
		{"cross database", "SELECT p.id, g.name FROM photos p JOIN gear.items g ON g.id = p.gear_item_id;", []string{`qualified table "gear.items"`}},
		{"backquoted cross database", "SELECT * FROM `gear`.`items`;", []string{`qualified table "gear.items"`}},
		{"unknown table", "SELECT * FROM items;", []string{`table "items" is not in`}},
		{"comment ignored", "-- FROM gear.items\nSELECT * FROM photos;", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := lint(fixture(t, c.queries))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("problems = %v, want %d", got, len(c.want))
			}
			for i := range c.want {
				if !strings.Contains(got[i], c.want[i]) {
					t.Errorf("problem[%d] = %q, want containing %q", i, got[i], c.want[i])
				}
			}
		})
	}
}
