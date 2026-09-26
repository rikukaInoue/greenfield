// Package localauthz は擬似ReBACのローカル実装（Authorizer / Lister / RelationWriter）。
// OpenFGA + authzサービス（Phase 3.2）の差し替え前に、開発の日常で使う。
//
// AllowAll で開発すると「認可のあるコードが検証されないまま蓄積し、本番アダプタを差した日に一斉に壊れる」
// ため、本物らしく厳しい側に寄せる: 所有者タプルがなければ拒否し、ListAccessible は所有物だけを列挙する。
// タプル書き込みの呼び忘れや ListAccessible の迂回が、開発中に目に見えて壊れる（conventions/internal-04）。
//
// 保存先はサービスのDBではなく専用のストア（別 *sql.DB）である。本番の authz サービスと同様に
// サービスのトランザクションへ参加しないため、「タプルは書けたが業務側がロールバックした」孤児タプルを
// 再現できる（#6）。driver は cmd 側が注入するため、core は database/sql（標準ライブラリ）にしか依存しない。
package localauthz

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/rikukaInoue/greenfield/core/authz"
)

// PlatformObject は platform operator を表すタプルの object。FGAモデルの `type platform` に対応する。
const PlatformObject = "platform:main"

// Mapping は action → 必要な relation の対応。
// 本番では authzサービス内に閉じる知識であり、プロダクトには語彙（action名・resource type名）だけを見せる。
// ローカル実装でも同じ位置づけで持つ（配線時に cmd から渡す）。
type Mapping map[string]string

// grants は FGAモデルの導出規則を手で解いた表。キーが要求する relation、
// 値が「その relation を満たす、直接タプルとして保存されうる relation 群」。
//
//	type photo
//	  relations
//	    define parent: [platform]
//	    define owner: [user]
//	    define viewer: owner or operator from parent
//	    define editor: owner or operator from parent
//
// viewer / editor は直接付与せず owner から導出する。加えて platform operator でも満たされる
// （fromParent を参照）。
var grants = map[string][]string{
	"owner":  {"owner"},
	"viewer": {"owner"},
	"editor": {"owner"},
}

// fromParent は `operator from parent` で満たされる relation。
var fromParent = map[string]bool{"viewer": true, "editor": true}

// Store は擬似ReBACのタプル置き場。
type Store struct {
	db      *sql.DB
	mapping Mapping
}

// New は Store を返す。db はサービスのDBとは別の接続（別database）であること。
func New(db *sql.DB, m Mapping) *Store {
	return &Store{db: db, mapping: m}
}

// Can は認可判定。所有者であるか、platform operator であれば許可する。
// FGAモデルの `define viewer: owner or operator from parent` を手で解いたもの。
func (s *Store) Can(ctx context.Context, req authz.Request) (authz.Result, error) {
	subject := req.Subject
	if subject == "" {
		p, ok := authz.PrincipalFrom(ctx)
		if !ok {
			return authz.Result{}, nil // 未認証は不許可（認証ミドルウェアが先に401にする）
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
	// operator from parent: platform operator は配下の全リソースに対して viewer / editor を持つ
	if fromParent[relation] {
		op, err := s.has(ctx, subject, "operator", PlatformObject)
		return authz.Result{Allowed: op}, err
	}
	return authz.Result{}, nil
}

// ListAccessible はアクセスできるリソースIDを列挙する。一覧は WHERE IN でこの結果を使う
// （Repositoryで全件取ってからフィルタする逃げ道を作らない）。
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
		if op { // platform operator は全件（所有者タプルの存在をもって「リソースがある」と見なす）
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

// WriteRelations はタプルを書き込む。同一タプルの重複適用は無害（自然冪等）であり、
// これは本番 authzサービスの `tuples:write` が保証する性質と同じ（Eventual化への備え）。
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
