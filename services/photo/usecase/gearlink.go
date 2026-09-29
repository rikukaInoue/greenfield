package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

// 同期コマンド「使用機材の紐付け」の送り側（4.4、docs/02-architecture.md）。
// 相手（gear）の結果が今の分岐を決めるため Eventual にはできない。かといって
// Atomic に載せると tx が外部応答を待つ（internal-03 §2.2 が禁じる形）。
// そこで: pending を Atomic で確定 → tx 外で冪等キー付きコマンド → 結果を別の Atomic で反映。
// gear 停止中は pending のまま残り、ReclaimGearLinks が冪等キー照会で確定させる（check #11）。

// ErrLinkNotFound は gear がそのキーのコマンドを受けていないことを表す。
// 回収ジョブはこれを見て**同じキーで**再送する。
var ErrLinkNotFound = errors.New("photo: 紐付けの記録が無い")

// GearLinkResult は gear の判定結果。
type GearLinkResult struct {
	// Status は linked / rejected（gear の usecase が定める契約）。
	Status string
	Reason string
}

// GearLink は gear への同期コマンドの差し込み口。実装（gear-client + M2M）は app が注入する。
type GearLink interface {
	// Link は紐付けコマンドを送る。リトライは**同じ key** で呼ぶこと（gear 側の重複排除の鍵）。
	Link(ctx context.Context, itemID, photoID int64, key string) (GearLinkResult, error)
	// Get は受理記録をキーで照会する。未受理なら ErrLinkNotFound。
	Get(ctx context.Context, key string) (GearLinkResult, error)
}

// newLinkKey は紐付けコマンドの冪等キーを採番する。
func newLinkKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("photo: 乱数が読めない: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// settleGearLink は gear の結果を別の Atomic で photo に反映する。
// 行をロックし直してから遷移するのは、回収ジョブと同時に走っても二重確定しないため。
func (c *PhotoCommands) settleGearLink(ctx context.Context, id domain.PhotoID, res GearLinkResult) error {
	return c.atomic.Do(ctx, func(ctx context.Context) error {
		p, err := c.photos.Get(ctx, id)
		if err != nil {
			return err
		}
		var terr error
		switch res.Status {
		case "linked":
			terr = p.ConfirmGearLink()
		case "rejected":
			terr = p.RejectGearLink()
		default:
			return fmt.Errorf("photo: gear の応答が読めない: status=%q", res.Status)
		}
		if errors.Is(terr, domain.ErrLinkNotPending) {
			// すでに確定済み（回収ジョブとの競合）。何もしないのが正しい
			return nil
		}
		if terr != nil {
			return terr
		}
		return c.photos.Save(ctx, p)
	})
}

// tryLinkGear は tx 外でコマンドを送り、結果を反映する。届かなければ pending のまま残す
// （失敗ではない——それが pending 状態パターンの意味）。
func (c *PhotoCommands) tryLinkGear(ctx context.Context, photo *domain.Photo) {
	itemID := photo.GearItemID()
	if itemID == nil || photo.GearLinkStatus() != domain.GearLinkPending {
		return
	}
	res, err := c.gear.Link(ctx, *itemID, int64(photo.ID()), photo.GearLinkKey())
	if err != nil {
		slog.WarnContext(ctx, "紐付けコマンドが届かない（pending のまま。回収ジョブが確定させる）",
			"photo_id", photo.ID(), "error", err)
		return
	}
	if err := c.settleGearLink(ctx, photo.ID(), res); err != nil {
		slog.ErrorContext(ctx, "紐付け結果の反映に失敗（pending のまま）", "photo_id", photo.ID(), "error", err)
		return
	}
	// settle は別 tx でフレッシュな行に対して確定する（回収ジョブとの競合対策）。
	// 呼び出し元が応答に使う手元のエンティティにも同じ遷移を映す——ここを忘れると
	// 「DB は確定済みなのに応答だけ pending」という混乱した応答になる（実測で踏んだ）
	switch res.Status {
	case "linked":
		_ = photo.ConfirmGearLink()
	case "rejected":
		_ = photo.RejectGearLink()
	}
}

// ReclaimGearLinks は pending のまま残った紐付けを冪等キー照会で確定させる（check #11）。
// gear が受けていれば（linked / rejected）その結果を反映し、受けていなければ（404）
// **同じキー**で再送する——キーを変えると gear 側の重複排除が効かず二重紐付けの余地が生まれる。
func (c *PhotoCommands) ReclaimGearLinks(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	pending, err := c.photos.ListPendingGearLinks(ctx, time.Now().Add(-olderThan), limit)
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, p := range pending {
		res, err := c.gear.Get(ctx, p.GearLinkKey())
		if errors.Is(err, ErrLinkNotFound) {
			itemID := p.GearItemID()
			if itemID == nil {
				// 機材が無いのに pending は作れないはずだが、来たら記録して飛ばす
				slog.ErrorContext(ctx, "pending なのに gear_item_id が無い", "photo_id", p.ID())
				continue
			}
			res, err = c.gear.Link(ctx, *itemID, int64(p.ID()), p.GearLinkKey())
		}
		if err != nil {
			// gear がまだ落ちている等。pending のまま次回に回す
			slog.WarnContext(ctx, "紐付けの回収に失敗（次回に回す）", "photo_id", p.ID(), "error", err)
			continue
		}
		if err := c.settleGearLink(ctx, p.ID(), res); err != nil {
			slog.ErrorContext(ctx, "紐付け結果の反映に失敗", "photo_id", p.ID(), "error", err)
			continue
		}
		settled++
	}
	return settled, nil
}
