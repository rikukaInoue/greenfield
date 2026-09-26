package usecase

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

// PhotoView は一覧・詳細で返す読み取り用の形。Entity を経由しない。
type PhotoView struct {
	ID      int64
	OwnerID string
	Caption string
	// Title は改名中の新カラム。表示に使うかはフラグで決まる（contract 後に消す）
	Title      string
	Visibility string
	GearItemID *int64
	Status     string
	ObjectKey  string
	SizeBytes  int64
	// ImageURL は署名付きの取得URL。Read Model の組み立て後に付与する。
	ImageURL  string
	CreatedAt string
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
	images     ImageStore
	authorizer authz.Authorizer
	lister     authz.Lister
}

// NewPhotoQueries は PhotoQueries を組み立てる。
func NewPhotoQueries(reader PhotoReader, images ImageStore, authorizer authz.Authorizer, lister authz.Lister) *PhotoQueries {
	return &PhotoQueries{reader: reader, images: images, authorizer: authorizer, lister: lister}
}

// viewTTL は画像取得用の署名URLの有効期限。
const viewTTL = 10 * time.Minute

// withImageURL は署名付きの取得URLを付与する。鍵がない行はそのまま返す。
func (q *PhotoQueries) withImageURL(ctx context.Context, v PhotoView) PhotoView {
	if v.ObjectKey == "" {
		return v
	}
	url, err := q.images.PresignGet(ctx, v.ObjectKey, viewTTL)
	if err != nil {
		return v // 表示できないだけで一覧全体は返す
	}
	v.ImageURL = url
	return v
}

// view は表示用の最終形にする。
func (q *PhotoQueries) view(ctx context.Context, v PhotoView) PhotoView {
	return q.withImageURL(ctx, v)
}

func (q *PhotoQueries) views(ctx context.Context, vs []PhotoView) []PhotoView {
	for i := range vs {
		vs[i] = q.view(ctx, vs[i])
	}
	return vs
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
	v, err := q.reader.Detail(ctx, id)
	if err != nil {
		return PhotoView{}, err
	}
	return q.view(ctx, v), nil
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
	vs, err := q.reader.ListByIDs(ctx, ids, limit)
	if err != nil {
		return nil, err
	}
	return q.views(ctx, vs), nil
}

// ListForOperator は全ユーザーの写真を返す。オペレータ向けで、権限は呼び出し側が確認する。
func (q *PhotoQueries) ListForOperator(ctx context.Context, ownerSubject string, limit int) ([]PhotoView, error) {
	var (
		vs  []PhotoView
		err error
	)
	if ownerSubject != "" {
		vs, err = q.reader.ListByOwner(ctx, ownerSubject, limit)
	} else {
		vs, err = q.reader.ListAll(ctx, limit)
	}
	if err != nil {
		return nil, err
	}
	return q.views(ctx, vs), nil
}

// 参照系は署名URLの付与以外にオブジェクトストレージへ触らない。

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
	if v.Visibility != string(domain.Public) || v.Status != string(domain.Ready) {
		return PhotoView{}, ErrNotFound
	}
	return q.view(ctx, v), nil
}

// PublicByGearItem は機材に紐づく公開済みの写真を返す。
func (q *PhotoQueries) PublicByGearItem(ctx context.Context, gearItemID int64, limit int) ([]PhotoView, error) {
	vs, err := q.reader.ListPublicByGearItem(ctx, gearItemID, limit)
	if err != nil {
		return nil, err
	}
	return q.views(ctx, vs), nil
}
