package migrations

import (
	"context"
	"os"
	"testing"
	"time"
)

// testDSN はローカルの mysql（make db-up）を指す。到達できなければスキップする。
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PHOTO_MIGRATE_DSN")
	if dsn == "" {
		dsn = "photo_migrate:photo_migrate@tcp(127.0.0.1:3306)/photo"
	}
	db, _, err := openDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("mysql not reachable (%v); run `mise run db:up`", err)
	}
	return dsn
}

// ロック待ちタイムアウトが接続のセッション変数として効いていること（ロック行列事故の防止、internal-05）。
func TestOpenDBSetsLockWaitTimeouts(t *testing.T) {
	db, _, err := openDB(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var lockWait, innodbLockWait int
	if err := db.QueryRow("SELECT @@SESSION.lock_wait_timeout, @@SESSION.innodb_lock_wait_timeout").Scan(&lockWait, &innodbLockWait); err != nil {
		t.Fatal(err)
	}
	if lockWait != lockWaitTimeout || innodbLockWait != lockWaitTimeout {
		t.Errorf("session timeouts = (%d, %d), want (%d, %d)", lockWait, innodbLockWait, lockWaitTimeout, lockWaitTimeout)
	}
}

// 履歴テーブルが系統ごとに分離されていること。
func TestStatusUsesSeparateHistoryTables(t *testing.T) {
	dsn := testDSN(t)
	db, _, err := openDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []Series{Expand, Contract} {
		if _, _, err := Status(dsn, s); err != nil {
			t.Fatalf("status %s: %v", s, err)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", "photo_migrations_"+string(s)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("history table photo_migrations_%s not found", s)
		}
	}
}
