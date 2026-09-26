// columncheck は改名中の新旧カラムの一致を検算する。
//
//	go run ./dev/columncheck -old caption -new title
//	go run ./dev/columncheck -old caption -new title -watch 2s
//	go run ./dev/columncheck -old caption -new title -exists-only
//
// 二重書きとバックフィルが正しければ一致率は 100% になる。
//
// 終了コード: 0 = 一致（または -exists-only で両方存在）、1 = 不一致、2 = カラムが無い。
// 呼び出し側（ドリル）が「まだ expand していない」と「一致しない」を区別できるようにしてある。
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
		dsn        = flag.String("dsn", envOr("PHOTO_DSN", "photo_app:photo_app@tcp(127.0.0.1:3306)/photo"), "接続先")
		table      = flag.String("table", "photos", "対象テーブル")
		oldCol     = flag.String("old", "", "旧カラム")
		newCol     = flag.String("new", "", "新カラム")
		watch      = flag.Duration("watch", 0, "指定すると間隔をおいて繰り返す")
		existsOnly = flag.Bool("exists-only", false, "両カラムの存在だけを確かめる")
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
		missing, err := missingColumns(db, *table, *oldCol, *newCol)
		if err != nil {
			fail(err)
		}
		if len(missing) > 0 {
			for _, c := range missing {
				fmt.Printf("%s.%s は存在しない\n", *table, c)
			}
			if *watch == 0 {
				os.Exit(2)
			}
			time.Sleep(*watch)
			continue
		}
		if *existsOnly {
			fmt.Printf("%s.%s と %s.%s はどちらも存在する\n", *table, *oldCol, *table, *newCol)
			return
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

// missingColumns は指定のカラムのうち存在しないものを返す。
func missingColumns(db *sql.DB, table string, columns ...string) ([]string, error) {
	var missing []string
	for _, c := range columns {
		var n int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
			table, c).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			missing = append(missing, c)
		}
	}
	return missing, nil
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
