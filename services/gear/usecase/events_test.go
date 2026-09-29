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

type fakeReplica struct {
	upserts []PhotoReplicaRow
	deletes []int64
	err     error
}

func (f *fakeReplica) Upsert(_ context.Context, row PhotoReplicaRow) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, row)
	return nil
}

func (f *fakeReplica) Delete(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.deletes = append(f.deletes, id)
	return nil
}

func events() (*PhotoEvents, *fakeInbox, *fakeReplica) {
	inbox := &fakeInbox{seen: map[string]bool{}}
	replica := &fakeReplica{}
	return NewPhotoEvents(passAtomic{}, inbox, replica), inbox, replica
}

func published(id string) Event {
	return Event{ID: id, Type: "photo.published", AggregateID: "photo:8",
		Payload: []byte(`{"id":8,"gear_item_id":1,"caption":"作例","created_at":"2026-01-01T00:00:00.000000Z"}`)}
}

func TestProcessAppliesOnce(t *testing.T) {
	p, _, replica := events()
	ev := published("e1")

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
	if len(replica.upserts) != 1 {
		t.Fatalf("副作用が %d 回（1回のはず）", len(replica.upserts))
	}
}

func TestPublishedBuildsReplica(t *testing.T) {
	p, _, replica := events()
	if _, err := p.Process(context.Background(), published("e1")); err != nil {
		t.Fatal(err)
	}
	r := replica.upserts[0]
	if r.PhotoID != 8 || r.GearItemID != 1 || r.Caption != "作例" {
		t.Fatalf("複製の内容が違う: %+v", r)
	}
}

func TestPublishedWithoutGearItemIsAcceptedWithoutReplica(t *testing.T) {
	// 機材に紐づかない写真は gear の関心の外。受理（inbox 記録）はするが複製は作らない
	p, _, replica := events()
	ev := Event{ID: "e2", Type: "photo.published", AggregateID: "photo:9",
		Payload: []byte(`{"id":9,"caption":"無関係","created_at":"2026-01-01T00:00:00.000000Z"}`)}
	applied, err := p.Process(context.Background(), ev)
	if err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if len(replica.upserts) != 0 {
		t.Fatalf("複製が作られている: %+v", replica.upserts)
	}
}

func TestDeletedRemovesReplica(t *testing.T) {
	p, _, replica := events()
	ev := Event{ID: "e3", Type: "photo.deleted", AggregateID: "photo:8", Payload: []byte(`{"id":8}`)}
	if _, err := p.Process(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(replica.deletes) != 1 || replica.deletes[0] != 8 {
		t.Fatalf("削除が反映されない: %+v", replica.deletes)
	}
}

func TestBrokenPayloadIsError(t *testing.T) {
	// 読めない payload は「処理できなかった」= 削除せず再配信に任せる（握りつぶさない）
	p, _, _ := events()
	ev := Event{ID: "e4", Type: "photo.published", Payload: []byte(`{broken`)}
	if _, err := p.Process(context.Background(), ev); err == nil {
		t.Fatal("壊れた payload が通る")
	}
}

func TestReplicaFailurePropagates(t *testing.T) {
	// 複製に書けないなら inbox 記録ごと転がす（同一 tx）。次の再配信で再試行される
	inbox := &fakeInbox{seen: map[string]bool{}}
	p := NewPhotoEvents(passAtomic{}, inbox, &fakeReplica{err: errors.New("db down")})
	if _, err := p.Process(context.Background(), published("e5")); err == nil {
		t.Fatal("複製の失敗が握りつぶされている")
	}
}

func TestProcessRequiresEventID(t *testing.T) {
	p, _, _ := events()
	if _, err := p.Process(context.Background(), Event{Type: "photo.published"}); err == nil {
		t.Fatal("ID の無いイベントが通る（inbox で弾けない）")
	}
}

func TestProcessInboxFailurePropagates(t *testing.T) {
	// inbox に書けないのは「処理できなかった」。呼び出し側は削除せず再配信に任せる
	p := NewPhotoEvents(passAtomic{}, &fakeInbox{err: errors.New("db down")}, &fakeReplica{})
	if _, err := p.Process(context.Background(), Event{ID: "e6", Type: "t"}); err == nil {
		t.Fatal("inbox の失敗が握りつぶされている")
	}
}

func TestProcessUnknownTypeIsAccepted(t *testing.T) {
	// 送り手が種別を増やしても受け手は壊れない（受理して無視）
	p, _, replica := events()
	applied, err := p.Process(context.Background(), Event{ID: "e7", Type: "photo.brand_new"})
	if err != nil || !applied {
		t.Fatalf("未知種別: applied=%v err=%v", applied, err)
	}
	if len(replica.upserts)+len(replica.deletes) != 0 {
		t.Fatal("未知種別で複製が動いている")
	}
}
