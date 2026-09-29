package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/services/gear/domain"
)

type fakeRepo struct{ created *domain.GearItem }

func (f *fakeRepo) Create(_ context.Context, item *domain.GearItem) error {
	item.SetID(42)
	f.created = item
	return nil
}

type passAtomic struct{}

func (passAtomic) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type fakeCatalog struct {
	refs []PhotoRef
	err  error
	got  int64
}

func (f *fakeCatalog) PublicPhotosByItem(_ context.Context, itemID int64, _ int) ([]PhotoRef, error) {
	f.got = itemID
	return f.refs, f.err
}

type fakeReader struct{ view ItemView }

func (f *fakeReader) Detail(context.Context, int64) (ItemView, error) { return f.view, nil }
func (f *fakeReader) List(context.Context, int) ([]ItemView, error)   { return []ItemView{f.view}, nil }

func authed(sub string) context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{Subject: sub, Kind: authz.PrincipalUser})
}

func TestCreateRequiresPrincipal(t *testing.T) {
	c := NewItemCommands(passAtomic{}, &fakeRepo{})
	if _, err := c.Create(context.Background(), CreateItemInput{Kind: "camera", Name: "X"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("未認証で作れてしまう: %v", err)
	}
}

func TestCreateValidatesViaDomain(t *testing.T) {
	c := NewItemCommands(passAtomic{}, &fakeRepo{})
	if _, err := c.Create(authed("u1"), CreateItemInput{Kind: "spaceship", Name: "X"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("未知の kind が通る: %v", err)
	}
	if _, err := c.Create(authed("u1"), CreateItemInput{Kind: "camera", Name: strings.Repeat("x", 121)}); !errors.Is(err, ErrInvalid) {
		t.Fatal("121文字の name が通る")
	}
}

func TestCreateRecordsCreator(t *testing.T) {
	repo := &fakeRepo{}
	c := NewItemCommands(passAtomic{}, repo)
	item, err := c.Create(authed("alice-sub"), CreateItemInput{Kind: "camera", Name: "X-T5", Maker: "FUJIFILM"})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID() != 42 || repo.created.CreatedBy() != "alice-sub" {
		t.Fatalf("id=%d createdBy=%q", item.ID(), repo.created.CreatedBy())
	}
}

func TestDetailMergesPhotos(t *testing.T) {
	cat := &fakeCatalog{refs: []PhotoRef{{ID: 7, Caption: "c"}}}
	q := NewItemQueries(&fakeReader{view: ItemView{ID: 3, Name: "X"}}, cat)
	d, err := q.Detail(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if cat.got != 3 || len(d.Photos) != 1 || d.Photos[0].ID != 7 {
		t.Fatalf("作例が併合されていない: got=%d photos=%v", cat.got, d.Photos)
	}
}

func TestDetailFailsWhenCatalogFails(t *testing.T) {
	// 半端な応答（photos だけ欠落）を返さない。空と失敗を呼び出し側が区別できなくなる
	q := NewItemQueries(&fakeReader{}, &fakeCatalog{err: errors.New("photo down")})
	if _, err := q.Detail(context.Background(), 1); err == nil {
		t.Fatal("作例の取得失敗が握りつぶされている")
	}
}
