// Package internalapi は internal リスナー（:8091、サービス間（client_credentials）。プライベートネットワークのみ）のハンドラ。
// 呼び出し主体ごとにリスナーを分けるのは、要求するAAL・レート制限・監査・到達経路が異なるため
// （conventions/api-design.md §3.2）。パスプレフィックスによる分離は採らない。
// ディレクトリ名を internal にしないのは、Goの internal パッケージ規則（親配下からしかimport不可）と衝突し
// app/ から配線できなくなるため（規約 internal-01 の handler/internal/ からの意図的な逸脱）。
package internalapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/problem"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// Deps はハンドラが使う差し込み口。実装は app/ が注入する。
// ハンドラは interface しか見ないため、本番アダプタへの差し替えで本ファイルは変わらない（#18）。
type Deps struct {
	Links *usecase.LinkCommands
}

type handlers struct {
	deps Deps
}

// Register は internal リスナーのルートを登録する。
// コマンドはユースケース単位（:verb / 名詞サブリソース）、クエリは Read Model 単位の GET とし、
// OperationID は対応する usecase 名に一致させる（生成クライアントのメソッド名になる）。
func Register(api httpapi.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api.Huma, huma.Operation{
		OperationID: "LinkPhoto",
		Method:      http.MethodPost,
		Path:        "/items/{id}:link-photo",
		Summary:     "写真と機材を紐付ける（同期コマンド）",
		Description: "photo からの同期コマンド。Idempotency-Key ごと結果を永続化し、同じキーの再送には最初の結果を返す（docs/02-architecture.md、4.4）。",
		Tags:        []string{"links"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, h.linkPhoto)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "GetPhotoLink",
		Method:      http.MethodGet,
		Path:        "/photo-links/{key}",
		Summary:     "紐付けの受理記録を冪等キーで照会する",
		Description: "photo の回収ジョブが pending を確定させるために使う。404 は「コマンドが届いていない」を意味し、同じキーでの再送を促す。",
		Tags:        []string{"links"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, h.getPhotoLink)
}

// Link は紐付け結果の応答表現。
type Link struct {
	Key     string `json:"key" doc:"冪等キー（photo が採番）"`
	ItemID  int64  `json:"item_id" doc:"機材ID"`
	PhotoID int64  `json:"photo_id" doc:"写真ID（photo サービスの ID）"`
	Status  string `json:"status" enum:"linked,rejected" doc:"結果"`
	Reason  string `json:"reason,omitempty" doc:"rejected の理由（item_not_found 等）"`
}

type LinkPhotoInput struct {
	ID  int64  `path:"id" minimum:"1" doc:"機材ID"`
	Key string `header:"Idempotency-Key" required:"true" doc:"photo が採番する冪等キー。リトライは同じ値で送る"`

	Body struct {
		PhotoID int64 `json:"photo_id" minimum:"1" doc:"紐付ける写真のID"`
	}
}

type LinkPhotoOutput struct {
	Body Link
}

func (h *handlers) linkPhoto(ctx context.Context, in *LinkPhotoInput) (*LinkPhotoOutput, error) {
	link, err := h.deps.Links.Link(ctx, in.Key, in.ID, in.Body.PhotoID)
	if err != nil {
		return nil, toHTTP(err)
	}
	return &LinkPhotoOutput{Body: fromLink(link)}, nil
}

type GetPhotoLinkInput struct {
	Key string `path:"key" maxLength:"64" doc:"冪等キー"`
}

type GetPhotoLinkOutput struct {
	Body Link
}

func (h *handlers) getPhotoLink(ctx context.Context, in *GetPhotoLinkInput) (*GetPhotoLinkOutput, error) {
	link, err := h.deps.Links.GetLink(ctx, in.Key)
	if err != nil {
		return nil, toHTTP(err)
	}
	return &GetPhotoLinkOutput{Body: fromLink(link)}, nil
}

func fromLink(l usecase.PhotoLink) Link {
	return Link{Key: l.Key, ItemID: l.ItemID, PhotoID: l.PhotoID, Status: l.Status, Reason: l.Reason}
}

func toHTTP(err error) error {
	switch {
	case errors.Is(err, usecase.ErrNotFound):
		return problem.New(http.StatusNotFound, "gear.link_not_found", "紐付けの記録が無い（コマンドは届いていない）")
	case errors.Is(err, usecase.ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, "gear.invalid", err.Error())
	default:
		return problem.New(http.StatusInternalServerError, problem.CodeInternal, "内部エラー")
	}
}
