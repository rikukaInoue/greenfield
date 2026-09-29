package consistency

import (
	"context"
	"errors"
	"testing"
)

// Publish / MarkProcessed が Atomic.Do の外で呼ばれたら、最初の実行で落ちること（ADR 0012）。
// 「業務データはコミット済みだが送信予定がない」を静かに作らせないためのガードであり、
// この2つのエラーはイベント基盤の保証そのものを守っている。
func TestPublishOutsideTxFails(t *testing.T) {
	err := Outbox{}.Publish(context.Background(), Event{Type: "t", AggregateID: "a"})
	if !errors.Is(err, ErrPublishOutsideTx) {
		t.Fatalf("tx 無しで Publish が通ってしまう: %v", err)
	}
}

func TestMarkProcessedOutsideTxFails(t *testing.T) {
	err := Inbox{}.MarkProcessed(context.Background(), Event{ID: "e1", Type: "t"})
	if !errors.Is(err, ErrPublishOutsideTx) {
		t.Fatalf("tx 無しで MarkProcessed が通ってしまう: %v", err)
	}
}

func TestNewEventIDShape(t *testing.T) {
	a, b := NewEventID(), NewEventID()
	if len(a) != 32 || a == b {
		t.Fatalf("イベントIDの形が想定外: %q %q", a, b)
	}
}
