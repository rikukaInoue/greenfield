package usecase

import (
	"context"
	"errors"
	"testing"
)

type fakeLinkRepo struct {
	items map[int64]bool
	rows  map[string]PhotoLink
}

func newFakeLinkRepo(items ...int64) *fakeLinkRepo {
	m := map[int64]bool{}
	for _, id := range items {
		m[id] = true
	}
	return &fakeLinkRepo{items: m, rows: map[string]PhotoLink{}}
}

func (f *fakeLinkRepo) Insert(_ context.Context, link PhotoLink) (bool, error) {
	if _, ok := f.rows[link.Key]; ok {
		return false, nil
	}
	f.rows[link.Key] = link
	return true, nil
}

func (f *fakeLinkRepo) Get(_ context.Context, key string) (PhotoLink, error) {
	l, ok := f.rows[key]
	if !ok {
		return PhotoLink{}, ErrNotFound
	}
	return l, nil
}

func (f *fakeLinkRepo) ItemExists(_ context.Context, id int64) (bool, error) {
	return f.items[id], nil
}

func TestLinkAcceptsExistingItem(t *testing.T) {
	c := NewLinkCommands(passAtomic{}, newFakeLinkRepo(1))
	l, err := c.Link(context.Background(), "k1", 1, 100)
	if err != nil || l.Status != LinkStatusLinked {
		t.Fatalf("l=%+v err=%v", l, err)
	}
}

func TestLinkRejectsUnknownItemButRecordsIt(t *testing.T) {
	// 拒否も受理の記録として残す。残さないと回収の照会が 404 になり再送が繰り返される
	repo := newFakeLinkRepo()
	c := NewLinkCommands(passAtomic{}, repo)
	l, err := c.Link(context.Background(), "k1", 99, 100)
	if err != nil || l.Status != LinkStatusRejected || l.Reason != LinkReasonItemNotFound {
		t.Fatalf("l=%+v err=%v", l, err)
	}
	if _, err := c.GetLink(context.Background(), "k1"); err != nil {
		t.Fatalf("拒否が照会できない: %v", err)
	}
}

func TestLinkIsIdempotentByKey(t *testing.T) {
	// 同じキーの再送は**最初の結果**を返す。判定し直すと結果が再送タイミング依存になる
	repo := newFakeLinkRepo() // item 99 は存在しない → 1回目は rejected
	c := NewLinkCommands(passAtomic{}, repo)
	first, _ := c.Link(context.Background(), "k1", 99, 100)
	repo.items[99] = true // その後 item ができたとしても
	second, err := c.Link(context.Background(), "k1", 99, 100)
	if err != nil || second.Status != first.Status {
		t.Fatalf("再送で結果が変わった: %+v → %+v (err=%v)", first, second, err)
	}
}

func TestLinkRequiresKey(t *testing.T) {
	c := NewLinkCommands(passAtomic{}, newFakeLinkRepo(1))
	if _, err := c.Link(context.Background(), "", 1, 100); !errors.Is(err, ErrInvalid) {
		t.Fatalf("キー無しが通る: %v", err)
	}
}

func TestGetLinkUnknownKeyIsNotFound(t *testing.T) {
	c := NewLinkCommands(passAtomic{}, newFakeLinkRepo(1))
	if _, err := c.GetLink(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未受理の照会: %v", err)
	}
}
