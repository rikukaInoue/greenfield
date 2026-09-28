package localauthz_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/authz/localauthz"
)

var mapping = localauthz.Mapping{
	"photo.view":   "viewer",
	"photo.edit":   "editor",
	"photo.delete": "owner",
}

// store はローカルの mysql（mise run db:up）を使う。到達できなければスキップする。
func store(t *testing.T) (*localauthz.Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("LOCALAUTHZ_DSN")
	if dsn == "" {
		dsn = "localauthz:localauthz@tcp(127.0.0.1:13306)/localauthz"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("mysql に到達できない (%v); `mise run db:up` を実行すること", err)
	}
	t.Cleanup(func() { db.Close() })
	return localauthz.New(db, mapping), db
}

// reset はテスト対象のタプルだけを消す（他テストと並行しても壊れないよう object で絞る）。
func reset(t *testing.T, db *sql.DB, objects ...string) {
	t.Helper()
	for _, o := range objects {
		if _, err := db.Exec("DELETE FROM relation_tuples WHERE object = ?", o); err != nil {
			t.Fatal(err)
		}
	}
}

func userCtx(sub string) context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{Subject: sub, Kind: authz.PrincipalUser})
}

// 所有者だけが自分の写真を見られる。タプルがなければ拒否する（AllowAll にしない）。
func TestOwnerOnly(t *testing.T) {
	s, db := store(t)
	reset(t, db, "photo:t1")

	ctx := userCtx("alice")
	req := authz.Request{Action: "photo.view", ResourceType: "photo", ResourceID: "t1"}

	got, err := s.Can(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Allowed {
		t.Fatal("タプルがないのに許可された（ローカル実装が緩すぎる）")
	}

	if err := s.WriteRelations(ctx, []authz.Tuple{{Subject: authz.UserRef("alice"), Relation: "owner", Object: "photo:t1"}}); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Can(ctx, req); err != nil || !got.Allowed {
		t.Fatalf("所有者が拒否された: %v %v", got, err)
	}
	if got, err = s.Can(userCtx("bob"), req); err != nil || got.Allowed {
		t.Fatalf("他人に許可された: %v %v", got, err)
	}
}

// platform operator は専用機構なしに（`operator from parent` の解決で）配下を見られる。
func TestPlatformOperator(t *testing.T) {
	s, db := store(t)
	reset(t, db, "photo:t2", localauthz.PlatformObject)

	ctx := userCtx("alice")
	if err := s.WriteRelations(ctx, []authz.Tuple{{Subject: authz.UserRef("alice"), Relation: "owner", Object: "photo:t2"}}); err != nil {
		t.Fatal(err)
	}
	view := authz.Request{Action: "photo.view", ResourceType: "photo", ResourceID: "t2"}

	if got, _ := s.Can(userCtx("op"), view); got.Allowed {
		t.Fatal("operator タプルがないのに許可された")
	}
	if err := s.WriteRelations(ctx, []authz.Tuple{{Subject: authz.UserRef("op"), Relation: "operator", Object: localauthz.PlatformObject}}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Can(userCtx("op"), view); err != nil || !got.Allowed {
		t.Fatalf("platform operator が拒否された: %v %v", got, err)
	}
	// owner を直接要求する action は operator では通らない（`define delete: owner` のモデルどおり）
	del := authz.Request{Action: "photo.delete", ResourceType: "photo", ResourceID: "t2"}
	if got, _ := s.Can(userCtx("op"), del); got.Allowed {
		t.Fatal("operator が owner 限定の action を通した")
	}
}

// ListAccessible は自分の分だけを返す。一覧は必ずこの結果を WHERE IN に使う。
func TestListAccessible(t *testing.T) {
	s, db := store(t)
	reset(t, db, "photo:t3", "photo:t4", localauthz.PlatformObject)

	ctx := userCtx("alice")
	if err := s.WriteRelations(ctx, []authz.Tuple{
		{Subject: authz.UserRef("alice"), Relation: "owner", Object: "photo:t3"},
		{Subject: authz.UserRef("bob"), Relation: "owner", Object: "photo:t4"},
	}); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ListAccessible(ctx, "photo.view", "photo")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(ids, "t3") || contains(ids, "t4") {
		t.Fatalf("alice の一覧 = %v, want t3 のみ", ids)
	}

	// 未認証は空（nil ではなく空スライス。呼び出し側が WHERE IN で空集合を扱えるように）
	if ids, err := s.ListAccessible(context.Background(), "photo.view", "photo"); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("未認証の一覧 = %v (%v), want 空スライス", ids, err)
	}

	// platform operator は全件見える
	if err := s.WriteRelations(ctx, []authz.Tuple{{Subject: authz.UserRef("op"), Relation: "operator", Object: localauthz.PlatformObject}}); err != nil {
		t.Fatal(err)
	}
	ids, err = s.ListAccessible(userCtx("op"), "photo.view", "photo")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(ids, "t3") || !contains(ids, "t4") {
		t.Fatalf("operator の一覧 = %v, want t3 と t4 を含む", ids)
	}
}

// 同一タプルの重複適用は無害（自然冪等）。本番 authzサービスの tuples:write と同じ性質。
func TestWriteIsIdempotent(t *testing.T) {
	s, db := store(t)
	reset(t, db, "photo:t5")

	ctx := userCtx("alice")
	tuples := []authz.Tuple{{Subject: authz.UserRef("alice"), Relation: "owner", Object: "photo:t5"}}
	for range 3 {
		if err := s.WriteRelations(ctx, tuples); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM relation_tuples WHERE object = 'photo:t5'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("タプル数 = %d, want 1", n)
	}
	// 存在しないタプルの削除も無害
	if err := s.DeleteRelations(ctx, []authz.Tuple{{Subject: authz.UserRef("zzz"), Relation: "owner", Object: "photo:t5"}}); err != nil {
		t.Fatal(err)
	}
}

// マッピングにない action はエラーにする（語彙の取りこぼしを黙って許可にしない）。
func TestUnknownAction(t *testing.T) {
	s, _ := store(t)
	if _, err := s.Can(userCtx("alice"), authz.Request{Action: "photo.unknown", ResourceType: "photo", ResourceID: "x"}); err == nil {
		t.Fatal("未知の action がエラーにならなかった")
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
