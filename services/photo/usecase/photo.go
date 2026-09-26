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
	ActionDelete  = "photo.delete"
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
	ListStalePending(ctx context.Context, before time.Time, limit int) ([]*domain.Photo, error)
}

// uploadTTL は署名URLの有効期限。
const uploadTTL = 15 * time.Minute

// フラグ名。`<種類>.<機能名>` の形で、種類は寿命を表す。
const (
	// FlagDisableUploads はストレージ障害時の縮退用キルスイッチ（長期）。
	FlagDisableUploads = "ops.photo_disable_uploads"
)

// PhotoCommands は写真の更新系ユースケース。
type PhotoCommands struct {
	atomic     Atomic
	photos     PhotoRepository
	images     ImageStore
	authorizer authz.Authorizer
	relations  authz.RelationWriter
	// faults は検証用の失敗注入。本番では常に空。
	faults FaultInjector
}

// NewPhotoCommands は PhotoCommands を組み立てる。
func NewPhotoCommands(atomic Atomic, photos PhotoRepository, images ImageStore, authorizer authz.Authorizer, relations authz.RelationWriter, faults FaultInjector) *PhotoCommands {
	if faults == nil {
		faults = NoFaults{}
	}
	return &PhotoCommands{atomic: atomic, photos: photos, images: images, authorizer: authorizer, relations: relations, faults: faults}
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
	if flags.Bool(ctx, FlagDisableUploads) {
		return nil, ErrUploadsDisabled
	}
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return nil, ErrForbidden
	}
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
	var photo *domain.Photo
	err := c.atomic.Do(ctx, func(ctx context.Context) error {
		p, err := c.photos.Get(ctx, id)
		if err != nil {
			return err
		}
		info, err := c.images.Stat(ctx, p.ObjectKey())
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
		photo = p
		return nil
	})
	return photo, err
}

// DeleteByOwner は所有者の写真を全て削除し、対応するタプルも落とす。
func (c *PhotoCommands) DeleteByOwner(ctx context.Context, ownerSubject string) (int, error) {
	var deleted int
	err := c.atomic.Do(ctx, func(ctx context.Context) error {
		n, err := c.photos.DeleteByOwner(ctx, ownerSubject)
		deleted = n
		return err
	})
	return deleted, err
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
