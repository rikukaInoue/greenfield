// Package internalapi は internal リスナー（サービス間）のハンドラ。
// パッケージ名が internalapi なのは Go の internal 規則を避けるため（docs/adr/0002-handler-internalapi.md）。
package internalapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/problem"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// Deps はハンドラが使う依存。実装は app/ が注入する。
type Deps struct {
	Queries *usecase.PhotoQueries
}

type handlers struct {
	deps Deps
}

// Register は internal リスナーのルートを登録する。
func Register(api httpapi.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api.Huma, huma.Operation{
		OperationID: "GetPhotoForService",
		Method:      http.MethodGet,
		Path:        "/photos/{id}",
		Summary:     "写真を取得する（サービス間）",
		Description: "gear が作例の表示に用いる。公開データのみを返す。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, h.getPhotoForService)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "ListPhotosByGearItem",
		Method:      http.MethodGet,
		Path:        "/gear-items/{gear_item_id}/photos",
		Summary:     "機材に紐づく作例の一覧（サービス間）",
		Description: "呼び出し側の N+1 を避けるための Batch 取得API。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden},
	}, h.listPhotosByGearItem)
}

// ServicePhoto はサービス間で公開する写真の表現。
type ServicePhoto struct {
	ID         int64  `json:"id" example:"1" doc:"写真ID"`
	OwnerID    string `json:"owner_id" example:"u_01H..." doc:"投稿者のSubject"`
	Caption    string `json:"caption" doc:"キャプション"`
	GearItemID *int64 `json:"gear_item_id,omitempty" doc:"使用機材（gear の item ID）"`
	CreatedAt  string `json:"created_at" format:"date-time" doc:"投稿時刻"`
}

type GetPhotoForServiceInput struct {
	ID int64 `path:"id" minimum:"1" doc:"写真ID"`
}

type GetPhotoForServiceOutput struct {
	Body ServicePhoto
}

type ListPhotosByGearItemInput struct {
	GearItemID int64 `path:"gear_item_id" minimum:"1" doc:"機材（item）ID"`
	Limit      int   `query:"limit,omitempty" minimum:"1" maximum:"200" default:"50" doc:"取得件数"`
}

type ListPhotosByGearItemOutput struct {
	Body struct {
		Photos []ServicePhoto `json:"photos" doc:"作例の一覧"`
	}
}

func (h *handlers) getPhotoForService(ctx context.Context, in *GetPhotoForServiceInput) (*GetPhotoForServiceOutput, error) {
	v, err := h.deps.Queries.PublicDetail(ctx, in.ID)
	if err != nil {
		return nil, notFoundOrInternal(err)
	}
	return &GetPhotoForServiceOutput{Body: servicePhoto(v)}, nil
}

func (h *handlers) listPhotosByGearItem(ctx context.Context, in *ListPhotosByGearItemInput) (*ListPhotosByGearItemOutput, error) {
	views, err := h.deps.Queries.PublicByGearItem(ctx, in.GearItemID, in.Limit)
	if err != nil {
		return nil, notFoundOrInternal(err)
	}
	out := &ListPhotosByGearItemOutput{}
	out.Body.Photos = make([]ServicePhoto, 0, len(views))
	for _, v := range views {
		out.Body.Photos = append(out.Body.Photos, servicePhoto(v))
	}
	return out, nil
}

func servicePhoto(v usecase.PhotoView) ServicePhoto {
	return ServicePhoto{
		ID: v.ID, OwnerID: v.OwnerID, Caption: v.Caption,
		GearItemID: v.GearItemID, CreatedAt: v.CreatedAt,
	}
}

func notFoundOrInternal(err error) error {
	if errors.Is(err, usecase.ErrNotFound) {
		return problem.New(http.StatusNotFound, "photo.not_found", "写真が見つからない")
	}
	return problem.New(http.StatusInternalServerError, problem.CodeInternal, "内部エラー")
}
