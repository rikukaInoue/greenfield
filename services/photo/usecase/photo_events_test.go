package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// captureEventual は受けたイベントを記録する。
type captureEventual struct {
	events []usecase.Event
	err    error
}

func (c *captureEventual) Publish(_ context.Context, ev usecase.Event) error {
	if c.err != nil {
		return c.err
	}
	c.events = append(c.events, ev)
	return nil
}

func readyPhoto(t *testing.T, repo *stubRepo) domain.PhotoID {
	t.Helper()
	size := int64(8)
	gear := int64(42)
	p := domain.Restore(domain.Restored{
		ID: 0, OwnerSubject: "alice-sub", Caption: "作例", Visibility: domain.Private,
		GearItemID: &gear, ObjectKey: "photos/x.png", ContentType: "image/png",
		SizeBytes: &size, Status: domain.Ready, CreatedAt: time.Now(),
	})
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p.ID()
}

func TestPublishEmitsEvent(t *testing.T) {
	repo := &stubRepo{}
	id := readyPhoto(t, repo)
	eventual := &captureEventual{}
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, repo, &stubImages{},
		&stubAuthorizer{allow: map[string]bool{usecase.ActionPublish: true}}, &stubRelations{}, eventual, nil)

	if _, err := cmds.Publish(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(eventual.events) != 1 {
		t.Fatalf("イベントが %d 件（1件のはず）", len(eventual.events))
	}
	ev := eventual.events[0]
	if ev.Type != usecase.EventPhotoPublished || ev.AggregateID != "photo:1" {
		t.Fatalf("type=%q aggregate=%q", ev.Type, ev.AggregateID)
	}
	var p usecase.PhotoPublishedPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	// 受け手が photo へ問い合わせずにレプリカを組める情報が入っていること（4.3 の前提）
	if p.ID != 1 || p.OwnerID != "alice-sub" || p.Caption != "作例" || p.GearItemID == nil || *p.GearItemID != 42 {
		t.Fatalf("payload が欠けている: %+v", p)
	}
}

func TestPublishFailsWhenEventualFails(t *testing.T) {
	// outbox に書けないなら公開ごと失敗させる。「公開されたのに送信予定がない」を作らない（ADR 0012）
	repo := &stubRepo{}
	id := readyPhoto(t, repo)
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, repo, &stubImages{},
		&stubAuthorizer{allow: map[string]bool{usecase.ActionPublish: true}}, &stubRelations{},
		&captureEventual{err: errors.New("outbox down")}, nil)

	if _, err := cmds.Publish(context.Background(), id); err == nil {
		t.Fatal("outbox への記録失敗が握りつぶされている")
	}
}

func TestDeleteByOwnerEmitsDeletedEvents(t *testing.T) {
	repo := &stubRepo{}
	id := readyPhoto(t, repo)
	eventual := &captureEventual{}
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, repo, &stubImages{},
		&stubAuthorizer{allow: map[string]bool{usecase.ActionOperate: true}}, &stubRelations{}, eventual, nil)

	n, err := cmds.DeleteByOwner(context.Background(), "alice-sub")
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if len(eventual.events) != 1 || eventual.events[0].Type != usecase.EventPhotoDeleted {
		t.Fatalf("photo.deleted が出ていない: %+v", eventual.events)
	}
	var p usecase.PhotoDeletedPayload
	if err := json.Unmarshal(eventual.events[0].Payload, &p); err != nil || p.ID != int64(id) {
		t.Fatalf("payload: %+v err=%v", p, err)
	}
}
