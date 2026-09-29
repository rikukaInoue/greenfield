package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Event は他ドメインから届いた事実。core/consistency の Event と同じ形
// （usecase は実装に依存しないため、app が変換して渡す）。
type Event struct {
	ID          string
	Type        string
	AggregateID string
	Payload     []byte
}

// ErrDuplicateEvent は処理済みのイベントを表す（inbox が弾いた）。
var ErrDuplicateEvent = errors.New("gear: 処理済みのイベント")

// Inbox は受信イベントの重複排除。実装（core/consistency）は app が注入する。
type Inbox interface {
	// MarkProcessed はイベントIDを記録する。処理済みなら ErrDuplicateEvent。
	MarkProcessed(ctx context.Context, ev Event) error
}

// PhotoEvents は photo ドメインのイベントを gear へ反映するユースケース。
// 「inbox への記録 + 業務処理」を同一トランザクション（Atomic）で行い、
// その後に呼び出し側（SQS consumer）がメッセージを削除する（internal-03 §2.3）。
type PhotoEvents struct {
	atomic Atomic
	inbox  Inbox
}

// NewPhotoEvents は受信処理一式を組む。
func NewPhotoEvents(atomic Atomic, inbox Inbox) *PhotoEvents {
	return &PhotoEvents{atomic: atomic, inbox: inbox}
}

// Process はイベントを1件処理する。重複はスキップし applied=false を返す（無害化。check #9）。
// エラーは「処理できなかった」であり、呼び出し側は削除せず再配信に任せる。
func (p *PhotoEvents) Process(ctx context.Context, ev Event) (applied bool, err error) {
	if ev.ID == "" {
		return false, fmt.Errorf("gear: イベントIDが無い: %+v", ev)
	}
	err = p.atomic.Do(ctx, func(ctx context.Context) error {
		if err := p.inbox.MarkProcessed(ctx, ev); err != nil {
			return err
		}
		return p.apply(ctx, ev)
	})
	if errors.Is(err, ErrDuplicateEvent) {
		slog.InfoContext(ctx, "重複イベントをスキップ", "event_id", ev.ID, "type", ev.Type)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// apply はイベント種別ごとの業務処理。4.2 時点では受理の記録のみで、
// 表示レプリカ（ReplicaView）への反映は 4.3 で入る。
func (p *PhotoEvents) apply(ctx context.Context, ev Event) error {
	switch ev.Type {
	case "photo.published", "photo.deleted":
		slog.InfoContext(ctx, "イベントを適用", "event_id", ev.ID, "type", ev.Type, "aggregate", ev.AggregateID)
		return nil
	default:
		// 未知の種別は受理して無視する（送り手が種別を増やしても受け手が壊れない）。
		// 記録は inbox に残るので、後から必要になれば outbox からの再生で拾い直せる
		slog.WarnContext(ctx, "未知のイベント種別", "event_id", ev.ID, "type", ev.Type)
		return nil
	}
}
