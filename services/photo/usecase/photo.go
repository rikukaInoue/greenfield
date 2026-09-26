// Package usecase は photo のユースケースを定義する。トランザクション境界はこの層が持つ。
package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/rikukaInoue/greenfield/core/authz"
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

// PhotoRepository は Entity の永続化。実装は repository パッケージが持つ。
type PhotoRepository interface {
	Create(ctx context.Context, p *domain.Photo) error
	Get(ctx context.Context, id domain.PhotoID) (*domain.Photo, error)
	Save(ctx context.Context, p *domain.Photo) error
	DeleteByOwner(ctx context.Context, ownerSubject string) (int, error)
}

// PhotoCommands は写真の更新系ユースケース。
type PhotoCommands struct {
	atomic     Atomic
	photos     PhotoRepository
	authorizer authz.Authorizer
	relations  authz.RelationWriter
	// faults は検証用の失敗注入。本番では常に空。
	faults FaultInjector
}

// NewPhotoCommands は PhotoCommands を組み立てる。
func NewPhotoCommands(atomic Atomic, photos PhotoRepository, authorizer authz.Authorizer, relations authz.RelationWriter, faults FaultInjector) *PhotoCommands {
	if faults == nil {
		faults = NoFaults{}
	}
	return &PhotoCommands{atomic: atomic, photos: photos, authorizer: authorizer, relations: relations, faults: faults}
}

// CreatePhotoInput は投稿の入力。
type CreatePhotoInput struct {
	Caption    string
	Visibility string
	GearItemID *int64
}

// Create は写真を投稿し、所有者タプルを書き込む。両者は同じ Atomic の中で確定する。
func (c *PhotoCommands) Create(ctx context.Context, in CreatePhotoInput) (*domain.Photo, error) {
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
	photo, err := domain.NewPhoto(p.Subject, caption, visibility, in.GearItemID)
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
	return photo, nil
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
