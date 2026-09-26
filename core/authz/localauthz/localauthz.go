// Package localauthz は擬似ReBAC のローカル実装（Authorizer / Lister / RelationWriter）を提供する。
// OpenFGA + authzサービスの差し替え前に使う。所有者タプルがなければ拒否する。
// タプルはサービスDBとは別のストアに置く（docs/adr/0004-localauthz-separate-store.md）。
package localauthz

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/rikukaInoue/greenfield/core/authz"
)

// PlatformObject は platform operator のタプルが指す object。
const PlatformObject = "platform:main"

// Mapping は action から必要な relation への対応。合成ルートが渡す。
type Mapping map[string]string

// grants は FGA モデルの導出規則。キーが要求する relation、値がそれを満たす直接タプルの relation。
// viewer / editor は直接付与せず owner から導出する。
var grants = map[string][]string{
	"owner":    {"owner"},
	"viewer":   {"owner"},
	"editor":   {"owner"},
	"operator": {"operator"},
	"support":  {"support"},
}

// fromParent は platform operator でも満たされる relation。
var fromParent = map[string]bool{"viewer": true, "editor": true}

// Store は擬似ReBAC のタプル置き場。
type Store struct {
	db      *sql.DB
	mapping Mapping
}

// New は Store を返す。db はサービスのDBとは別の接続であること。
func New(db *sql.DB, m Mapping) *Store {
	return &Store{db: db, mapping: m}
}

// Can は所有者であるか、platform operator であれば許可する。
func (s *Store) Can(ctx context.Context, req authz.Request) (authz.Result, error) {
	subject := req.Subject
	if subject == "" {
		p, ok := authz.PrincipalFrom(ctx)
		if !ok {
			return authz.Result{}, nil // 未認証は不許可。通常は認証ミドルウェアが先に 401 にする
		}
		subject = principalRef(p)
	}
	relation, ok := s.mapping[req.Action]
	if !ok {
		return authz.Result{}, fmt.Errorf("localauthz: 未知の action %q（action→relation マッピングに追加すること）", req.Action)
	}
	if _, known := grants[relation]; !known {
		return authz.Result{}, fmt.Errorf("localauthz: 未知の relation %q（FGAモデルの導出表に追加すること）", relation)
	}
	object := authz.ObjectRef(req.ResourceType, req.ResourceID)

	for _, direct := range grants[relation] {
		ok, err := s.has(ctx, subject, direct, object)
		if err != nil {
			return authz.Result{}, err
		}
		if ok {
			return authz.Result{Allowed: true}, nil
		}
	}
	if fromParent[relation] {
		op, err := s.has(ctx, subject, "operator", PlatformObject)
		return authz.Result{Allowed: op}, err
	}
	return authz.Result{}, nil
}

// ListAccessible はアクセスできるリソースIDを列挙する。一覧は WHERE IN でこの結果を使う。
func (s *Store) ListAccessible(ctx context.Context, action, resourceType string) ([]string, error) {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return []string{}, nil
	}
	subject := principalRef(p)
	relation, ok := s.mapping[action]
	if !ok {
		return nil, fmt.Errorf("localauthz: 未知の action %q", action)
	}
	prefix := resourceType + ":"

	if fromParent[relation] {
		op, err := s.has(ctx, subject, "operator", PlatformObject)
		if err != nil {
			return nil, err
		}
		if op { // 所有者タプルの存在をもってリソースの存在と見なす
			return s.ids(ctx, "SELECT DISTINCT object FROM relation_tuples WHERE relation = 'owner' AND object LIKE ?", prefix+"%")
		}
	}
	direct := grants[relation]
	if len(direct) == 0 {
		return []string{}, nil
	}
	query := "SELECT DISTINCT object FROM relation_tuples WHERE subject = ? AND object LIKE ? AND relation IN (?" + strings.Repeat(", ?", len(direct)-1) + ")"
	args := []any{subject, prefix + "%"}
	for _, r := range direct {
		args = append(args, r)
	}
	return s.ids(ctx, query, args...)
}

// WriteRelations はタプルを書き込む。同一タプルの重複適用は無害。
func (s *Store) WriteRelations(ctx context.Context, tuples []authz.Tuple) error {
	for _, t := range tuples {
		if _, err := s.db.ExecContext(ctx,
			"INSERT INTO relation_tuples (subject, relation, object) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE subject = subject",
			t.Subject, t.Relation, t.Object); err != nil {
			return fmt.Errorf("localauthz: write %v: %w", t, err)
		}
	}
	return nil
}

// DeleteRelations はタプルを削除する。存在しないタプルの削除も無害。
func (s *Store) DeleteRelations(ctx context.Context, tuples []authz.Tuple) error {
	for _, t := range tuples {
		if _, err := s.db.ExecContext(ctx,
			"DELETE FROM relation_tuples WHERE subject = ? AND relation = ? AND object = ?",
			t.Subject, t.Relation, t.Object); err != nil {
			return fmt.Errorf("localauthz: delete %v: %w", t, err)
		}
	}
	return nil
}

func (s *Store) has(ctx context.Context, subject, relation, object string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM relation_tuples WHERE subject = ? AND relation = ? AND object = ?",
		subject, relation, object).Scan(&n)
	return n > 0, err
}

func (s *Store) ids(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var object string
		if err := rows.Scan(&object); err != nil {
			return nil, err
		}
		if _, id, found := strings.Cut(object, ":"); found {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func principalRef(p authz.Principal) string {
	if p.Kind == authz.PrincipalService {
		return authz.ServiceRef(p.ClientID)
	}
	return authz.UserRef(p.Subject)
}
