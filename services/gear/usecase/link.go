package usecase

import (
	"context"
	"fmt"
)

// 同期コマンド「使用機材の紐付け」の受け側（4.4、docs/02-architecture.md）。
// photo が Idempotency-Key を採番して呼び、gear は結果（linked / rejected）を
// キーごと永続化する。photo の回収ジョブがキーで照会して pending を確定させるため、
// 「受けたかどうか」を後から答えられることが要件そのもの。

// 紐付けの結果。
const (
	LinkStatusLinked   = "linked"
	LinkStatusRejected = "rejected"
)

// 拒否理由（機械可読の短い語。photo 側の表示・分岐が読む契約）。
const (
	LinkReasonItemNotFound = "item_not_found"
)

// PhotoLink は紐付けの受理記録。
type PhotoLink struct {
	Key     string
	ItemID  int64
	PhotoID int64
	Status  string
	Reason  string
}

// LinkRepository は photo_links への読み書き。
type LinkRepository interface {
	// Insert は記録を試みる。同じキーが既にあれば inserted=false（上書きしない）。
	Insert(ctx context.Context, link PhotoLink) (inserted bool, err error)
	// Get はキーで引く。無ければ ErrNotFound。
	Get(ctx context.Context, key string) (PhotoLink, error)
	ItemExists(ctx context.Context, itemID int64) (bool, error)
}

// LinkCommands は紐付けコマンド。
type LinkCommands struct {
	atomic Atomic
	links  LinkRepository
}

// NewLinkCommands は紐付けコマンド一式を組む。
func NewLinkCommands(atomic Atomic, links LinkRepository) *LinkCommands {
	return &LinkCommands{atomic: atomic, links: links}
}

// Link は写真と機材の紐付けを受理する。同じキーの再送には**最初の結果**を返す（冪等）。
// リトライのたびに判定し直すと「1回目 rejected、2回目 linked」のような不定が生まれ、
// photo 側の確定が再送のタイミング依存になる。
func (c *LinkCommands) Link(ctx context.Context, key string, itemID, photoID int64) (PhotoLink, error) {
	if key == "" {
		return PhotoLink{}, fmt.Errorf("%w: Idempotency-Key が無い", ErrInvalid)
	}
	if itemID <= 0 || photoID <= 0 {
		return PhotoLink{}, fmt.Errorf("%w: item_id / photo_id が不正", ErrInvalid)
	}
	// 判定（読み取り）は Do の前（internal-03 §2.2 の配置）
	exists, err := c.links.ItemExists(ctx, itemID)
	if err != nil {
		return PhotoLink{}, err
	}
	link := PhotoLink{Key: key, ItemID: itemID, PhotoID: photoID, Status: LinkStatusLinked}
	if !exists {
		// 拒否も**受理の記録**として残す。残さないと回収ジョブの照会が 404 になり、
		// photo は「届いていない」と誤解して再送を繰り返す
		link.Status = LinkStatusRejected
		link.Reason = LinkReasonItemNotFound
	}
	var out PhotoLink
	err = c.atomic.Do(ctx, func(ctx context.Context) error {
		inserted, err := c.links.Insert(ctx, link)
		if err != nil {
			return err
		}
		if inserted {
			out = link
			return nil
		}
		existing, err := c.links.Get(ctx, key)
		if err != nil {
			return err
		}
		out = existing
		return nil
	})
	return out, err
}

// GetLink はキーで受理記録を引く（回収ジョブの照会用）。無ければ ErrNotFound——
// それは「コマンドが届いていない」ことを意味し、photo は同じキーで再送してよい。
func (c *LinkCommands) GetLink(ctx context.Context, key string) (PhotoLink, error) {
	return c.links.Get(ctx, key)
}
