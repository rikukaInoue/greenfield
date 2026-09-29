package consistency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// Event は他ドメインへ公表する事実。サービス側 usecase の Event と同じ形
// （usecase は core に依存しないため、app が変換して渡す）。
type Event struct {
	ID          string
	Type        string
	AggregateID string
	Payload     []byte

	// DedupID はバス側の重複排除（MessageDeduplicationId）に使う値。空なら ID を使う。
	// 通常の配送では設定しない。Republish だけが run ごとに別の値を入れる——
	// バスの5分窓は**事故の二重送信**を抑えるためのもので、意図した再生まで
	// 黙って落とされては再構築が空振りする（実測: check #10 で踏んだ）。
	// 受信側の重複排除の鍵は常に ID（inbox）。
	DedupID string
}

// NewEventID はイベントIDを採番する（UUID v4 相当の乱数16バイト）。
// 送信側が採番し、受信側の inbox・バスの MessageDeduplicationId の両方で使う。
func NewEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("consistency: 乱数が読めない: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// ErrPublishOutsideTx は Publish が Atomic.Do の外で呼ばれたことを表す。
// 別 tx で outbox に書けてしまうと「業務データはコミット済みだが送信予定がない」が作れる。
// 規約（internal-03 §2.2 第四項 / ADR 0012）を人の注意力ではなく機構で守る。
var ErrPublishOutsideTx = errors.New("consistency: Publish は Atomic.Do の中でしか呼べない")

// Outbox は Eventual の標準実装（internal-03 §2.3）。業務データと同一トランザクションで
// outbox テーブルへ INSERT し、実際の送信は Relay に委ねる。
// テーブルは各サービスが自DBに持つ（DDL は ddl/outbox.sql をマイグレーションへ複写）。
type Outbox struct{}

// Publish はイベントを outbox に記録する。ctx にトランザクションが無ければエラー。
// event.ID が空なら採番する。
func (Outbox) Publish(ctx context.Context, event Event) error {
	tx, ok := TxFrom(ctx)
	if !ok {
		return ErrPublishOutsideTx
	}
	if event.ID == "" {
		event.ID = NewEventID()
	}
	if event.Type == "" || event.AggregateID == "" {
		return fmt.Errorf("consistency: イベントに Type / AggregateID が無い: %+v", event)
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO outbox (event_id, event_type, aggregate_id, payload) VALUES (?, ?, ?, ?)`,
		event.ID, event.Type, event.AggregateID, event.Payload)
	if err != nil {
		return fmt.Errorf("consistency: outbox への記録: %w", err)
	}
	return nil
}

// ErrDuplicateEvent は処理済みのイベントを表す。受信側はこれを見てスキップする（無害化）。
var ErrDuplicateEvent = errors.New("consistency: 処理済みのイベント")

// Inbox は受信側の重複排除（internal-03 §2.3「inboxが主たる防御」）。
// MarkProcessed を業務処理と同一トランザクション（Atomic.Do）で呼ぶことで、
// 「イベントIDの記録 + 業務処理」が原子的になる。SQS の削除前に停止すれば
// メッセージは再配信されるが、記録済みの ID がここで弾く。
type Inbox struct{}

// MarkProcessed はイベントIDを inbox に記録する。処理済みなら ErrDuplicateEvent。
// Publish と同じ理由で tx 必須（別 tx で記録すると業務処理と一緒に転がらない）。
func (Inbox) MarkProcessed(ctx context.Context, event Event) error {
	tx, ok := TxFrom(ctx)
	if !ok {
		return ErrPublishOutsideTx
	}
	res, err := tx.ExecContext(ctx,
		`INSERT IGNORE INTO inbox (event_id, event_type) VALUES (?, ?)`,
		event.ID, event.Type)
	if err != nil {
		return fmt.Errorf("consistency: inbox への記録: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("consistency: inbox の結果: %w", err)
	}
	if n == 0 {
		return ErrDuplicateEvent
	}
	return nil
}
