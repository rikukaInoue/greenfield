package usecase

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

// PhotoView は一覧・詳細で返す読み取り用の形。Entity を経由しない。
type PhotoView struct {
	ID         int64
	OwnerID    string
	Caption    string
	Visibility string
	GearItemID *int64
	CreatedAt  string
}

// PhotoReader は Read Model の取得。実装は readmodel パッケージが持つ。
type PhotoReader interface {
	Detail(ctx context.Context, id domain.PhotoID) (PhotoView, error)
	ListByIDs(ctx context.Context, ids []int64, limit int) ([]PhotoView, error)
	ListByOwner(ctx context.Context, ownerSubject string, limit int) ([]PhotoView, error)
	ListAll(ctx context.Context, limit int) ([]PhotoView, error)
	ListPublicByGearItem(ctx context.Context, gearItemID int64, limit int) ([]PhotoView, error)
}

// PhotoQueries は写真の参照系ユースケース。トランザクションを通らない。
type PhotoQueries struct {
	reader     PhotoReader
	authorizer authz.Authorizer
	lister     authz.Lister
}

// NewPhotoQueries は PhotoQueries を組み立てる。
func NewPhotoQueries(reader PhotoReader, authorizer authz.Authorizer, lister authz.Lister) *PhotoQueries {
	return &PhotoQueries{reader: reader, authorizer: authorizer, lister: lister}
}

// Detail は1件の詳細を返す。見る権限がなければ ErrNotFound。
func (q *PhotoQueries) Detail(ctx context.Context, id domain.PhotoID, consistency authz.Consistency) (PhotoView, error) {
	res, err := q.authorizer.Can(ctx, authz.Request{
		Action: ActionView, ResourceType: ResourceType, ResourceID: fmt.Sprint(id), Consistency: consistency,
	})
	if err != nil {
		return PhotoView{}, err
	}
	if !res.Allowed {
		return PhotoView{}, ErrNotFound
	}
	return q.reader.Detail(ctx, id)
}

// List は主体が見られる写真を返す。ListAccessible が返した ID を WHERE IN で絞る。
func (q *PhotoQueries) List(ctx context.Context, limit int) ([]PhotoView, error) {
	refs, err := q.lister.ListAccessible(ctx, ActionView, ResourceType)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(refs))
	for _, r := range refs {
		id, err := strconv.ParseInt(r, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []PhotoView{}, nil
	}
	return q.reader.ListByIDs(ctx, ids, limit)
}

// ListForOperator は全ユーザーの写真を返す。オペレータ向けで、権限は呼び出し側が確認する。
func (q *PhotoQueries) ListForOperator(ctx context.Context, ownerSubject string, limit int) ([]PhotoView, error) {
	if ownerSubject != "" {
		return q.reader.ListByOwner(ctx, ownerSubject, limit)
	}
	return q.reader.ListAll(ctx, limit)
}

// CanOperate は主体が platform operator かを返す。オペレータ向け経路の入口で使う。
func (q *PhotoQueries) CanOperate(ctx context.Context) (bool, error) {
	res, err := q.authorizer.Can(ctx, authz.Request{
		Action: ActionOperate, ResourceType: "platform", ResourceID: "main",
	})
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}

// PublicDetail は公開済みの写真を1件返す。サービス間の経路から使う。
func (q *PhotoQueries) PublicDetail(ctx context.Context, id int64) (PhotoView, error) {
	v, err := q.reader.Detail(ctx, domain.PhotoID(id))
	if err != nil {
		return PhotoView{}, err
	}
	if v.Visibility != string(domain.Public) {
		return PhotoView{}, ErrNotFound
	}
	return v, nil
}

// PublicByGearItem は機材に紐づく公開済みの写真を返す。
func (q *PhotoQueries) PublicByGearItem(ctx context.Context, gearItemID int64, limit int) ([]PhotoView, error) {
	return q.reader.ListPublicByGearItem(ctx, gearItemID, limit)
}
