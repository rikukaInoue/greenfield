package usecase

import (
	"context"
	"encoding/json"
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

// PhotoReplicaRow は複製に持つ最小限のフィールド。依存するのは photo が公開すると
// 決めたイベントの契約であり、photo のテーブル構造ではない（internal-01 §ReplicaView）。
type PhotoReplicaRow struct {
	PhotoID    int64
	GearItemID int64
	Caption    string
	CreatedAt  string
}

// PhotoReplica は表示用複製の書き込み口。実装（replicaview パッケージ）は app が注入する。
// usecase から replicaview を直接 import することは depguard が禁止しており、
// この interface 越しの**書き込み**だけを許す（読み取り＝業務判断への利用は経路がない）。
type PhotoReplica interface {
	Upsert(ctx context.Context, row PhotoReplicaRow) error
	Delete(ctx context.Context, photoID int64) error
}

// PhotoEvents は photo ドメインのイベントを gear へ反映するユースケース。
// 「inbox への記録 + 業務処理」を同一トランザクション（Atomic）で行い、
// その後に呼び出し側（SQS consumer）がメッセージを削除する（internal-03 §2.3）。
type PhotoEvents struct {
	atomic  Atomic
	inbox   Inbox
	replica PhotoReplica
}

// NewPhotoEvents は受信処理一式を組む。
func NewPhotoEvents(atomic Atomic, inbox Inbox, replica PhotoReplica) *PhotoEvents {
	return &PhotoEvents{atomic: atomic, inbox: inbox, replica: replica}
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

// apply はイベント種別ごとの業務処理。表示レプリカ（photo_replica）への反映を行う（4.3）。
// inbox の記録と同一 tx なので、反映に失敗すれば記録ごと転がり再配信で再試行される。
func (p *PhotoEvents) apply(ctx context.Context, ev Event) error {
	switch ev.Type {
	case "photo.published":
		var body struct {
			ID         int64  `json:"id"`
			GearItemID *int64 `json:"gear_item_id"`
			Caption    string `json:"caption"`
			CreatedAt  string `json:"created_at"`
		}
		if err := json.Unmarshal(ev.Payload, &body); err != nil {
			return fmt.Errorf("gear: photo.published の payload が読めない: %w", err)
		}
		if body.GearItemID == nil {
			// 機材に紐づかない写真は gear の関心の外。受理だけして複製は作らない
			slog.InfoContext(ctx, "機材に紐づかない作例（複製せず受理）", "event_id", ev.ID, "photo_id", body.ID)
			return nil
		}
		slog.InfoContext(ctx, "イベントを適用", "event_id", ev.ID, "type", ev.Type, "aggregate", ev.AggregateID)
		return p.replica.Upsert(ctx, PhotoReplicaRow{
			PhotoID: body.ID, GearItemID: *body.GearItemID,
			Caption: body.Caption, CreatedAt: body.CreatedAt,
		})
	case "photo.deleted":
		var body struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(ev.Payload, &body); err != nil {
			return fmt.Errorf("gear: photo.deleted の payload が読めない: %w", err)
		}
		slog.InfoContext(ctx, "イベントを適用", "event_id", ev.ID, "type", ev.Type, "aggregate", ev.AggregateID)
		return p.replica.Delete(ctx, body.ID)
	default:
		// 未知の種別は受理して無視する（送り手が種別を増やしても受け手が壊れない）。
		// 記録は inbox に残るので、後から必要になれば outbox からの再生で拾い直せる
		slog.WarnContext(ctx, "未知のイベント種別", "event_id", ev.ID, "type", ev.Type)
		return nil
	}
}
