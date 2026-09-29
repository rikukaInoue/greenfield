package usecase

import (
	"encoding/json"
	"fmt"

	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

// 他ドメインへ公表するイベント種別。名前は `<domain>.<過去形の事実>`。
// ペイロードはイベント時点の事実の写しであり、受け手が photo へ問い合わせずに
// 表示レプリカを組めるだけの情報を持たせる（ReplicaView、4.3）。
const (
	// EventPhotoPublished は写真が公開されたこと。
	EventPhotoPublished = "photo.published"
	// EventPhotoDeleted は写真が削除されたこと。
	EventPhotoDeleted = "photo.deleted"
)

// PhotoPublishedPayload は photo.published のペイロード。
type PhotoPublishedPayload struct {
	ID         int64  `json:"id"`
	OwnerID    string `json:"owner_id"`
	Caption    string `json:"caption"`
	GearItemID *int64 `json:"gear_item_id,omitempty"`
	ObjectKey  string `json:"object_key"`
	CreatedAt  string `json:"created_at"`
}

// PhotoDeletedPayload は photo.deleted のペイロード。
type PhotoDeletedPayload struct {
	ID int64 `json:"id"`
}

// aggregateID は photo 集約のバス上の識別子（MessageGroupId になる）。
func aggregateID(id domain.PhotoID) string { return fmt.Sprintf("photo:%d", id) }

func publishedEvent(p *domain.Photo) (Event, error) {
	payload, err := json.Marshal(PhotoPublishedPayload{
		ID: int64(p.ID()), OwnerID: p.OwnerSubject(), Caption: string(p.Caption()),
		GearItemID: p.GearItemID(), ObjectKey: p.ObjectKey(),
		CreatedAt: p.CreatedAt().UTC().Format("2006-01-02T15:04:05.000000Z"),
	})
	if err != nil {
		return Event{}, fmt.Errorf("photo: イベントの encode: %w", err)
	}
	return Event{Type: EventPhotoPublished, AggregateID: aggregateID(p.ID()), Payload: payload}, nil
}

func deletedEvent(id domain.PhotoID) (Event, error) {
	payload, err := json.Marshal(PhotoDeletedPayload{ID: int64(id)})
	if err != nil {
		return Event{}, fmt.Errorf("photo: イベントの encode: %w", err)
	}
	return Event{Type: EventPhotoDeleted, AggregateID: aggregateID(id), Payload: payload}, nil
}
