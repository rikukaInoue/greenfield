// columncheck は改名中の新旧カラムの一致を検算する。
//
//	go run ./dev/columncheck -old caption -new title
//	go run ./dev/columncheck -old caption -new title -watch 2s
//
// 二重書きとバックフィルが正しければ一致率は 100% になる。
// 新カラムが存在しない（expand 前）場合はその旨を出して exit 0 で抜ける。
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type result struct {
	Table        string `json:"table"`
	Old          string `json:"old_column"`
	New          string `json:"new_column"`
	Total        int64  `json:"total"`
	NewIsNull    int64  `json:"new_is_null"`
	Mismatched   int64  `json:"mismatched"`
	MatchPercent string `json:"match_percent"`
}

func main() {
	var (
		dsn    = flag.String("dsn", envOr("PHOTO_DSN", "photo_app:photo_app@tcp(127.0.0.1:3306)/photo"), "接続先")
		table  = flag.String("table", "photos", "対象テーブル")
		oldCol = flag.String("old", "", "旧カラム")
		newCol = flag.String("new", "", "新カラム")
		watch  = flag.Duration("watch", 0, "指定すると間隔をおいて繰り返す")
	)
	flag.Parse()
	if *oldCol == "" || *newCol == "" {
		fmt.Fprintln(os.Stderr, "usage: columncheck -old <column> -new <column>")
		os.Exit(2)
	}

	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	for {
		exists, err := columnExists(db, *table, *newCol)
		if err != nil {
			fail(err)
		}
		if !exists {
			fmt.Printf("%s.%s は未作成（expand 前）\n", *table, *newCol)
			if *watch == 0 {
				return
			}
			time.Sleep(*watch)
			continue
		}
		r, err := check(db, *table, *oldCol, *newCol)
		if err != nil {
			fail(err)
		}
		out, _ := json.Marshal(r)
		fmt.Println(string(out))
		if *watch == 0 {
			// 不一致があれば失敗として扱う
			if r.Mismatched > 0 || r.NewIsNull > 0 {
				os.Exit(1)
			}
			return
		}
		time.Sleep(*watch)
	}
}

// check は全行について旧カラムと新カラムを突き合わせる。
// NULL の新カラムはバックフィル未完了として別に数える。
func check(db *sql.DB, table, oldCol, newCol string) (result, error) {
	q := fmt.Sprintf(`SELECT
	  COUNT(*),
	  SUM(CASE WHEN %[2]s IS NULL THEN 1 ELSE 0 END),
	  SUM(CASE WHEN %[2]s IS NOT NULL AND NOT (%[1]s <=> %[2]s) THEN 1 ELSE 0 END)
	FROM %[3]s`, quote(oldCol), quote(newCol), quote(table))

	var total int64
	var nulls, mismatched sql.NullInt64
	if err := db.QueryRow(q).Scan(&total, &nulls, &mismatched); err != nil {
		return result{}, err
	}
	r := result{Table: table, Old: oldCol, New: newCol, Total: total, NewIsNull: nulls.Int64, Mismatched: mismatched.Int64}
	switch {
	case total == 0:
		r.MatchPercent = "n/a"
	default:
		matched := total - r.NewIsNull - r.Mismatched
		r.MatchPercent = fmt.Sprintf("%.2f", float64(matched)*100/float64(total))
	}
	return r, nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	var n int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, column).Scan(&n)
	return n > 0, err
}

// quote は識別子をバッククォートで囲む。バッククォート自体は許さない。
func quote(id string) string {
	for _, r := range id {
		if r == '`' || r == 0 {
			fail(fmt.Errorf("識別子に使えない文字: %q", id))
		}
	}
	return "`" + id + "`"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "columncheck:", err)
	os.Exit(1)
}
