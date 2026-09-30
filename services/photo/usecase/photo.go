// Package usecase は photo のユースケースを定義する。トランザクション境界はこの層が持つ。
package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/flags"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

// action は認可判定の語彙。FGA モデルの relation へマッピングされる。
const (
	ActionView    = "photo.view"
	ActionEdit    = "photo.edit"
	ActionPublish = "photo.publish"
	ActionOperate = "platform.operate"
)

// ResourceType は認可判定のリソース種別。
const ResourceType = "photo"

// ErrNotFound は対象が存在しないか、主体に見えないことを表す。
var ErrNotFound = errors.New("photo: 見つからない")

// ErrForbidden は主体に権限がないことを表す。
var ErrForbidden = errors.New("photo: 権限がない")

// ErrObjectNotFound は画像オブジェクトが存在しないことを表す。
var ErrObjectNotFound = errors.New("photo: 画像オブジェクトがない")

// ErrUploadsDisabled はキルスイッチにより投稿が止まっていることを表す。
var ErrUploadsDisabled = errors.New("photo: 投稿を一時停止中")

// PhotoRepository は Entity の永続化。実装は repository パッケージが持つ。
type PhotoRepository interface {
	Create(ctx context.Context, p *domain.Photo) error
	Get(ctx context.Context, id domain.PhotoID) (*domain.Photo, error)
	Save(ctx context.Context, p *domain.Photo) error
	Delete(ctx context.Context, id domain.PhotoID) error
	DeleteByOwner(ctx context.Context, ownerSubject string) (int, error)
	ListByOwner(ctx context.Context, ownerSubject string) ([]*domain.Photo, error)
	ListStalePending(ctx context.Context, before time.Time, limit int) ([]*domain.Photo, error)
	ListPendingGearLinks(ctx context.Context, before time.Time, limit int) ([]*domain.Photo, error)
}

// uploadTTL は署名URLの有効期限。
const uploadTTL = 15 * time.Minute

// フラグ名。`<種類>_<機能名>` の形で、種類は寿命を表す。
// 区切りが "_" なのは AWS AppConfig のキー制約(^[a-z][a-zA-Z0-9_-]{0,63}$、"." 不可)に
// 合わせたため(7.5 実測)。マネージド基盤で使えない文字を規約に入れない。
const (
	// FlagDisableUploads はストレージ障害時の縮退用キルスイッチ（長期）。
	FlagDisableUploads = "ops_photo_disable_uploads"
)

// PhotoCommands は写真の更新系ユースケース。
type PhotoCommands struct {
	atomic     Atomic
	photos     PhotoRepository
	images     ImageStore
	authorizer authz.Authorizer
	relations  authz.RelationWriter
	eventual   Eventual
	gear       GearLink
	// faults は検証用の失敗注入。本番では常に空。
	faults FaultInjector
}

// NewPhotoCommands は PhotoCommands を組み立てる。
func NewPhotoCommands(atomic Atomic, photos PhotoRepository, images ImageStore, authorizer authz.Authorizer, relations authz.RelationWriter, eventual Eventual, gear GearLink, faults FaultInjector) *PhotoCommands {
	if faults == nil {
		faults = NoFaults{}
	}
	return &PhotoCommands{atomic: atomic, photos: photos, images: images, authorizer: authorizer, relations: relations, eventual: eventual, gear: gear, faults: faults}
}

// CanCreate は主体が今投稿できるかを返す。Create と同じ判定を使う。
func (c *PhotoCommands) CanCreate(ctx context.Context) bool {
	return c.checkCreate(ctx) == nil
}

func (c *PhotoCommands) checkCreate(ctx context.Context) error {
	if flags.Bool(ctx, FlagDisableUploads) {
		return ErrUploadsDisabled
	}
	if _, ok := authz.PrincipalFrom(ctx); !ok {
		return ErrForbidden
	}
	return nil
}

// CreatePhotoInput は投稿の入力。
type CreatePhotoInput struct {
	Caption     string
	Visibility  string
	GearItemID  *int64
	ContentType string
}

// CreatePhotoResult は投稿の結果。画像は署名URLへ直接 PUT してから Commit する。
type CreatePhotoResult struct {
	Photo  *domain.Photo
	Upload UploadTarget
}

// Create は写真のレコードを PendingUpload で作り、画像アップロード用の署名URLを返す。
// オブジェクトストレージは外部システムでロールバックできないため、鍵の予約だけを先に行い、
// 実体の存在確認は Commit で行う。
func (c *PhotoCommands) Create(ctx context.Context, in CreatePhotoInput) (*CreatePhotoResult, error) {
	if err := c.checkCreate(ctx); err != nil {
		return nil, err
	}
	p, _ := authz.PrincipalFrom(ctx)
	caption, err := domain.ParseCaption(in.Caption)
	if err != nil {
		return nil, err
	}
	visibility, err := domain.ParseVisibility(in.Visibility)
	if err != nil {
		return nil, err
	}
	key, err := c.images.NewKey(p.Subject, in.ContentType)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalid, err)
	}
	photo, err := domain.NewPhoto(p.Subject, caption, visibility, in.GearItemID, key, in.ContentType)
	if err != nil {
		return nil, err
	}
	if in.GearItemID != nil {
		// 使用機材の紐付けは同期コマンド（docs/02-architecture.md）。
		// pending は業務データと同一 tx で確定し、コマンド自体は tx の外で送る
		if err := photo.RequestGearLink(newLinkKey()); err != nil {
			return nil, err
		}
	}

	err = c.atomic.Do(ctx, func(ctx context.Context) error {
		if err := c.photos.Create(ctx, photo); err != nil {
			return err
		}
		if err := c.faults.Inject(FaultBeforeRelations); err != nil {
			return err
		}
		if err := c.relations.WriteRelations(ctx, []authz.Tuple{{
			Subject:  authz.UserRef(p.Subject),
			Relation: "owner",
			Object:   authz.ObjectRef(ResourceType, fmt.Sprint(photo.ID())),
		}}); err != nil {
			return err
		}
		return c.faults.Inject(FaultBeforeCommit)
	})
	if err != nil {
		return nil, err
	}
	// 紐付けコマンドは tx 外。届かなくても投稿は成立し、pending が回収ジョブに引き継がれる
	c.tryLinkGear(ctx, photo)

	upload, err := c.images.PresignPut(ctx, key, in.ContentType, uploadTTL)
	if err != nil {
		return nil, err
	}
	return &CreatePhotoResult{Photo: photo, Upload: upload}, nil
}

// CommitUpload は画像の存在を確認して Ready へ遷移させる。
// 実体がなければ遷移させないので、DB にあるが画像がない状態は表示経路に出ない。
func (c *PhotoCommands) CommitUpload(ctx context.Context, id domain.PhotoID) (*domain.Photo, error) {
	if err := c.can(ctx, ActionEdit, id); err != nil {
		return nil, err
	}
	// オブジェクトの存在確認は **tx の外**でやる。S3 は管理下の相手ではないので
	// Atomic の条件(2)「短いタイムアウトを付与できる」を満たさず、内部-03 自身が
	// 「Atomic に載せられない」と書いている。tx 内で呼ぶと FOR UPDATE の行ロックと
	// DB 接続を握ったまま外部の応答を待つことになる(#85 C-1)。
	//
	// 先に読むぶん「Stat の後に誰かがオブジェクトを消す」窓は開くが、その窓で困るのは
	// 「ready なのに実体がない」状態で、これは Reclaim が pending を掃除するのと同じ
	// 種類の後追い可能なズレ。ロック保持で全リクエストを詰まらせる代償のほうが高い。
	var (
		photo *domain.Photo
		key   string
	)
	if err := c.atomic.Do(ctx, func(ctx context.Context) error {
		p, err := c.photos.Get(ctx, id)
		if err != nil {
			return err
		}
		key = p.ObjectKey()
		return nil
	}); err != nil {
		return nil, err
	}
	info, err := c.images.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	err = c.atomic.Do(ctx, func(ctx context.Context) error {
		p, err := c.photos.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := p.CommitUpload(info.SizeBytes); err != nil {
			return err
		}
		if err := c.photos.Save(ctx, p); err != nil {
			return err
		}
		photo = p
		return nil
	})
	return photo, err
}

// Reclaim はアップロードが完了しないまま放置された写真を、オブジェクトごと削除する。
// オブジェクトが先に消えても行が残るだけなので、削除はオブジェクト → 行の順で行う。
func (c *PhotoCommands) Reclaim(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	stale, err := c.photos.ListStalePending(ctx, time.Now().Add(-olderThan), limit)
	if err != nil {
		return 0, err
	}
	var reclaimed int
	for _, p := range stale {
		if err := c.images.Delete(ctx, p.ObjectKey()); err != nil {
			return reclaimed, err
		}
		if err := c.photos.Delete(ctx, p.ID()); err != nil {
			return reclaimed, err
		}
		if err := c.relations.DeleteRelations(ctx, []authz.Tuple{{
			Subject:  authz.UserRef(p.OwnerSubject()),
			Relation: "owner",
			Object:   authz.ObjectRef(ResourceType, fmt.Sprint(p.ID())),
		}}); err != nil {
			return reclaimed, err
		}
		reclaimed++
	}
	return reclaimed, nil
}

// Publish は写真を公開する。
func (c *PhotoCommands) Publish(ctx context.Context, id domain.PhotoID) (*domain.Photo, error) {
	if err := c.can(ctx, ActionPublish, id); err != nil {
		return nil, err
	}
	var photo *domain.Photo
	err := c.atomic.Do(ctx, func(ctx context.Context) error {
		p, err := c.photos.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := p.Publish(); err != nil {
			return err
		}
		if err := c.photos.Save(ctx, p); err != nil {
			return err
		}
		// 公開の事実は Eventual で公表する。outbox への記録が業務データと
		// 同一 tx なので「公開されたのに送信予定がない」は作れない（ADR 0012）
		ev, err := publishedEvent(p)
		if err != nil {
			return err
		}
		if err := c.eventual.Publish(ctx, ev); err != nil {
			return err
		}
		photo = p
		return nil
	})
	return photo, err
}

// DeleteByOwner は所有者の写真を、オブジェクトとタプルまで含めて削除する。
//
// 以前は DB の行だけを消していた(#85 C-3)。行が消えると object_key を二度と
// 辿れないので、**全画像がバケットに残って回収不能**になり、owner タプルも
// 永久に残っていた。孤児タプルは無害ではない: localauthz の operator 経路は
// owner タプルをリソースの存在証明として使う(ListAccessible)。
//
// 認可は **operator 権限**で見る。アカウント削除はオペレータの操作であって
// 写真ごとの owner 権限ではない。owner relation にマップされる action
// (photo.edit 等)で判定すると、operator が継承するのは viewer / editor だけ
// なので、この経路は必ず拒否される(core/authz/localauthz の fromParent)。
//
// 順序は Reclaim と同じ「無害な側から」: オブジェクト → 行 → タプル。
// 途中で落ちたときに残るのは「行はあるが画像がない」(表示経路には status で
// 出ない)か「タプルだけ残る」で、どちらも後から掃除できる。逆順にすると
// 「行が無いのに画像が残る」= 辿れないゴミになる。
//
// オブジェクトの削除は tx の外でやる(C-1 と同じ理由。S3 は Atomic に載らない)。
func (c *PhotoCommands) DeleteByOwner(ctx context.Context, ownerSubject string) (int, error) {
	res, err := c.authorizer.Can(ctx, authz.Request{
		Action: ActionOperate, ResourceType: "platform", ResourceID: "main",
	})
	if err != nil {
		return 0, err
	}
	if !res.Allowed {
		return 0, ErrForbidden
	}

	photos, err := c.photos.ListByOwner(ctx, ownerSubject)
	if err != nil {
		return 0, err
	}
	for _, p := range photos {
		if err := c.images.Delete(ctx, p.ObjectKey()); err != nil {
			return 0, err
		}
	}

	var deleted int
	if err := c.atomic.Do(ctx, func(ctx context.Context) error {
		n, err := c.photos.DeleteByOwner(ctx, ownerSubject)
		if err != nil {
			return err
		}
		deleted = n
		// 削除も公表する（ReplicaView が公開レプリカから落とすため）。行の削除と同一 tx
		for _, p := range photos {
			ev, err := deletedEvent(p.ID())
			if err != nil {
				return err
			}
			if err := c.eventual.Publish(ctx, ev); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}

	tuples := make([]authz.Tuple, 0, len(photos))
	for _, p := range photos {
		tuples = append(tuples, authz.Tuple{
			Subject:  authz.UserRef(p.OwnerSubject()),
			Relation: "owner",
			Object:   authz.ObjectRef(ResourceType, fmt.Sprint(p.ID())),
		})
	}
	if len(tuples) > 0 {
		if err := c.relations.DeleteRelations(ctx, tuples); err != nil {
			// 行は消えている。タプルだけ残った状態は observable なので、
			// 呼び出し側に失敗として返して再実行させる(冪等に書いてある)
			return deleted, err
		}
	}
	return deleted, nil
}

func (c *PhotoCommands) can(ctx context.Context, action string, id domain.PhotoID) error {
	res, err := c.authorizer.Can(ctx, authz.Request{
		Action: action, ResourceType: ResourceType, ResourceID: fmt.Sprint(id),
	})
	if err != nil {
		return err
	}
	if !res.Allowed {
		return ErrNotFound // 存在を伏せる
	}
	return nil
}
