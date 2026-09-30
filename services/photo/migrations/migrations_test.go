package migrations

import (
	"context"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source"
)

// testDSN はローカルの mysql（make db-up）を指す。到達できなければスキップする。
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PHOTO_MIGRATE_DSN")
	if dsn == "" {
		dsn = "photo_migrate:photo_migrate@tcp(127.0.0.1:13306)/photo"
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

// fakeSource は source.Driver の First / Prev だけを持つ最小実装。
// resetDirty の判定は「source に何があるか」で決まるので、DB を使わずに固定できる。
type fakeSource struct {
	versions []uint // 昇順
}

func (s fakeSource) Open(string) (source.Driver, error) { return s, nil }
func (s fakeSource) Close() error                       { return nil }
func (s fakeSource) First() (uint, error) {
	if len(s.versions) == 0 {
		return 0, os.ErrNotExist
	}
	return s.versions[0], nil
}
func (s fakeSource) Prev(version uint) (uint, error) {
	for i, v := range s.versions {
		if v == version {
			if i == 0 {
				return 0, os.ErrNotExist
			}
			return s.versions[i-1], nil
		}
	}
	return 0, os.ErrNotExist // version 自体が source に無い
}
func (s fakeSource) Next(uint) (uint, error) { return 0, os.ErrNotExist }
func (s fakeSource) ReadUp(uint) (io.ReadCloser, string, error) {
	return nil, "", os.ErrNotExist
}
func (s fakeSource) ReadDown(uint) (io.ReadCloser, string, error) {
	return nil, "", os.ErrNotExist
}

// dirty が残ったバージョンが source に無いとき（失敗したマイグレーションを消した /
// 番号を変えた = 前方修正の普通の形）、**履歴を推測で書き換えない**。
//
// 以前は Prev のエラーを「失敗したのが最初のマイグレーション」と読んで Force(-1) し、
// 履歴を全消ししていた。次の実行は CREATE TABLE から始まって "already exists" で
// 落ち、以後どのデプロイも通らなくなる（check #24 で観測。ステージ 2.3 / #46）。
func TestResetDirtyDoesNotWipeHistoryWhenVersionIsGone(t *testing.T) {
	dsn := testDSN(t)
	db, _, err := openDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 実際の migrate インスタンスに、source だけ差し替えたものを渡す。
	// dirty = 4 / source = {1,2,3} の形を作る
	m, src, cleanup := testMigrate(t, dsn, fakeSource{versions: []uint{1, 2, 3}})
	defer cleanup()
	if err := m.Force(4); err != nil {
		t.Fatal(err)
	}
	if err := forceDirty(db, 4); err != nil {
		t.Fatal(err)
	}

	err = resetDirty(m, src)
	if err == nil {
		t.Fatal("dirty なバージョンが source に無いのに成功している（履歴を書き換えた疑い）")
	}
	if !strings.Contains(err.Error(), "source に無い") {
		t.Errorf("理由が伝わらない: %v", err)
	}
	// 履歴は触られていないこと（Force(-1) されていない）
	var version int
	var dirty bool
	if err := db.QueryRow("SELECT version, dirty FROM photo_migrations_expand").Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 4 || !dirty {
		t.Errorf("履歴が書き換わっている: version=%d dirty=%v（want 4 / true）", version, dirty)
	}
}

// 失敗したのが本当に最初のマイグレーションなら、戻し先は「何も適用していない」。
func TestResetDirtyClearsWhenFirstMigrationFailed(t *testing.T) {
	dsn := testDSN(t)
	db, _, err := openDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	m, src, cleanup := testMigrate(t, dsn, fakeSource{versions: []uint{1, 2, 3}})
	defer cleanup()
	if err := m.Force(1); err != nil {
		t.Fatal(err)
	}
	if err := forceDirty(db, 1); err != nil {
		t.Fatal(err)
	}
	if err := resetDirty(m, src); err != nil {
		t.Fatalf("resetDirty: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM photo_migrations_expand WHERE dirty = 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("dirty が残っている")
	}
}

// testMigrate は source だけ差し替えた migrate インスタンスを返す。
// 実 DB（expand の履歴テーブル）に対して resetDirty の判定を見るため。
func testMigrate(t *testing.T, dsn string, src source.Driver) (*migrate.Migrate, source.Driver, func()) {
	t.Helper()
	db, cfg, err := openDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{
		DatabaseName:    cfg.DBName,
		MigrationsTable: "photo_migrations_expand",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("fake", src, cfg.DBName, driver)
	if err != nil {
		t.Fatal(err)
	}
	// 実験で書き換えた履歴は戻す。ローカルの DB を壊したまま抜けない
	var version int
	var dirty bool
	_ = db.QueryRow("SELECT version, dirty FROM photo_migrations_expand").Scan(&version, &dirty)
	return m, src, func() {
		_, _ = db.Exec("DELETE FROM photo_migrations_expand")
		if version != 0 {
			_, _ = db.Exec("INSERT INTO photo_migrations_expand (version, dirty) VALUES (?, ?)", version, dirty)
		}
		m.Close()
		db.Close()
	}
}

// forceDirty は履歴の dirty フラグだけを立てる（Force は dirty=false で書くため）。
func forceDirty(db *sql.DB, version int) error {
	_, err := db.Exec("UPDATE photo_migrations_expand SET version = ?, dirty = 1", version)
	return err
}

// PendingCount の数え方(#200)。埋め込み一覧と適用済みバージョンの純粋な突き合わせ部分。
func TestPendingCountLogic(t *testing.T) {
	versions, err := embeddedVersions(Contract)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 {
		t.Fatal("contract の埋め込みが読めていない(0件はこのリポジトリでは前提崩れ)")
	}
	if n := pendingCount(versions, 0); n != len(versions) {
		t.Fatalf("未適用(applied=0)なら全件のはず: got %d want %d", n, len(versions))
	}
	max := versions[len(versions)-1]
	if n := pendingCount(versions, max); n != 0 {
		t.Fatalf("最新まで適用済みなら 0 のはず: got %d", n)
	}
	if n := pendingCount(versions, versions[0]); n != len(versions)-1 {
		t.Fatalf("1件適用済みなら残り %d のはず: got %d", len(versions)-1, n)
	}
}

// 履歴テーブルが無い database では「全件未適用」として数える(エラーにしない)。
// テーブル欠如を -1(観測不能)にすると、初回デプロイ前が永遠に欠測になる。
func TestPendingCountWithoutHistoryTable(t *testing.T) {
	testDSN(t) // 到達性チェック(届かなければ skip)だけ流用する
	root, _, err := openDB("root:root@tcp(127.0.0.1:13306)/mysql")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	if _, err := root.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS migrations_scratch"); err != nil {
		t.Fatal(err)
	}
	defer root.ExecContext(ctx, "DROP DATABASE migrations_scratch")
	db, _, err := openDB("root:root@tcp(127.0.0.1:13306)/migrations_scratch")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	n, err := PendingCount(ctx, db, Contract)
	if err != nil {
		t.Fatal(err)
	}
	versions, _ := embeddedVersions(Contract)
	if n != len(versions) {
		t.Fatalf("履歴テーブルの無い database では全件未適用のはず: got %d want %d", n, len(versions))
	}
}
