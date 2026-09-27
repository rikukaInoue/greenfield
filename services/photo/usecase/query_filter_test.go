package usecase_test

// #91 E: visibility の絞り込みが「件数を切った後」に効いていたため、公開写真が
// 取りこぼされていた。絞り込みは件数を切る前に効くこと(= usecase より下)を固定する。

import (
	"context"
	"testing"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// filterReader は SQL 側の振る舞いを再現する: visibility で絞ってから limit で切る。
type filterReader struct {
	rows []usecase.PhotoView
	// gotVisibility は下の層に絞り込みが届いたか。ハンドラで後から絞る形に
	// 戻ると空のままになる
	gotVisibility string
	gotLimit      int
}

func (r *filterReader) ListByIDs(_ context.Context, ids []int64, visibility string, limit int) ([]usecase.PhotoView, error) {
	r.gotVisibility, r.gotLimit = visibility, limit
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := make([]usecase.PhotoView, 0, len(r.rows))
	for _, v := range r.rows {
		if !want[v.ID] {
			continue
		}
		if visibility != "" && v.Visibility != visibility {
			continue
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *filterReader) Detail(context.Context, domain.PhotoID) (usecase.PhotoView, error) {
	return usecase.PhotoView{}, usecase.ErrNotFound
}
func (r *filterReader) ListByOwner(context.Context, string, int) ([]usecase.PhotoView, error) {
	return nil, nil
}
func (r *filterReader) ListAll(context.Context, int) ([]usecase.PhotoView, error) { return nil, nil }
func (r *filterReader) ListPublicByGearItem(context.Context, int64, int) ([]usecase.PhotoView, error) {
	return nil, nil
}

// allLister は渡した ID を全部「見える」として返す。
type allLister struct{ ids []string }

func (l allLister) ListAccessible(context.Context, string, string) ([]string, error) {
	return l.ids, nil
}

type allowAll struct{}

func (allowAll) Can(context.Context, authz.Request) (authz.Result, error) {
	return authz.Result{Allowed: true}, nil
}

func TestListDoesNotDropMatchesBehindTheLimit(t *testing.T) {
	// private が 2 枚先に並び、その後に public が 2 枚。limit=2 で public を求める。
	// 絞り込みが limit の後だと **0 枚**になる(これが #91 E のバグ)。
	reader := &filterReader{rows: []usecase.PhotoView{
		{ID: 1, Visibility: "private"},
		{ID: 2, Visibility: "private"},
		{ID: 3, Visibility: "public"},
		{ID: 4, Visibility: "public"},
	}}
	q := usecase.NewPhotoQueries(reader, &stubImages{}, allowAll{}, allLister{ids: []string{"1", "2", "3", "4"}})

	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})
	got, err := q.List(ctx, "public", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("public を %d 枚返した, want 2（limit の後に絞ると 0 枚になる）", len(got))
	}
	for _, v := range got {
		if v.Visibility != "public" {
			t.Errorf("private が混ざっている: %+v", v)
		}
	}
	// 絞り込みが下の層まで届いていること(ハンドラで後から絞る形への逆戻り検知)
	if reader.gotVisibility != "public" || reader.gotLimit != 2 {
		t.Errorf("下の層に届いた条件 = %q / %d, want public / 2", reader.gotVisibility, reader.gotLimit)
	}
}

// visibility 未指定なら全件(絞り込み無し)のまま。
func TestListWithoutVisibilityReturnsAll(t *testing.T) {
	reader := &filterReader{rows: []usecase.PhotoView{
		{ID: 1, Visibility: "private"},
		{ID: 2, Visibility: "public"},
	}}
	q := usecase.NewPhotoQueries(reader, &stubImages{}, allowAll{}, allLister{ids: []string{"1", "2"}})
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})

	got, err := q.List(ctx, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("%d 枚, want 2", len(got))
	}
}
