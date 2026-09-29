package usecase

import (
	"context"
	"errors"
	"testing"
)

type fakeInbox struct {
	seen map[string]bool
	err  error
}

func (f *fakeInbox) MarkProcessed(_ context.Context, ev Event) error {
	if f.err != nil {
		return f.err
	}
	if f.seen[ev.ID] {
		return ErrDuplicateEvent
	}
	f.seen[ev.ID] = true
	return nil
}

func TestProcessAppliesOnce(t *testing.T) {
	p := NewPhotoEvents(passAtomic{}, &fakeInbox{seen: map[string]bool{}})
	ev := Event{ID: "e1", Type: "photo.published", AggregateID: "photo:1"}

	applied, err := p.Process(context.Background(), ev)
	if err != nil || !applied {
		t.Fatalf("1回目: applied=%v err=%v", applied, err)
	}
	// 同一イベントの再配送（at-least-once）は無害化: エラーではなくスキップ（check #9）。
	// エラーにすると consumer が削除せず、同じメッセージが永遠に再配信される
	applied, err = p.Process(context.Background(), ev)
	if err != nil || applied {
		t.Fatalf("2回目: applied=%v err=%v", applied, err)
	}
}

func TestProcessRequiresEventID(t *testing.T) {
	p := NewPhotoEvents(passAtomic{}, &fakeInbox{seen: map[string]bool{}})
	if _, err := p.Process(context.Background(), Event{Type: "photo.published"}); err == nil {
		t.Fatal("ID の無いイベントが通る（inbox で弾けない）")
	}
}

func TestProcessInboxFailurePropagates(t *testing.T) {
	// inbox に書けないのは「処理できなかった」。呼び出し側は削除せず再配信に任せる
	p := NewPhotoEvents(passAtomic{}, &fakeInbox{err: errors.New("db down")})
	if _, err := p.Process(context.Background(), Event{ID: "e1", Type: "t"}); err == nil {
		t.Fatal("inbox の失敗が握りつぶされている")
	}
}

func TestProcessUnknownTypeIsAccepted(t *testing.T) {
	// 送り手が種別を増やしても受け手は壊れない（受理して無視）
	p := NewPhotoEvents(passAtomic{}, &fakeInbox{seen: map[string]bool{}})
	applied, err := p.Process(context.Background(), Event{ID: "e2", Type: "photo.brand_new"})
	if err != nil || !applied {
		t.Fatalf("未知種別: applied=%v err=%v", applied, err)
	}
}
