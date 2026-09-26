// Package internalapi は internal リスナー（:8081、サービス間（client_credentials）。プライベートネットワークのみ）のハンドラ。
// 呼び出し主体ごとにリスナーを分けるのは、要求するAAL・レート制限・監査・到達経路が異なるため
// （conventions/api-design.md §3.2）。パスプレフィックスによる分離は採らない。
// ディレクトリ名を internal にしないのは、Goの internal パッケージ規則（親配下からしかimport不可）と衝突し
// app/ から配線できなくなるため（規約 internal-01 の handler/internal/ からの意図的な逸脱）。
package internalapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/problem"
)

// Deps はハンドラが使う差し込み口。実装（localauthz / oidcauthn 等）は app/ が注入する。
// ハンドラは interface しか見ないため、本番アダプタへの差し替えで本ファイルは変わらない（#18）。
type Deps struct {
	Authorizer authz.Authorizer
	Lister     authz.Lister
	Assurance  authz.AssuranceChecker
}

type handlers struct {
	deps Deps
}

// Register は internal リスナーのルートを登録する。
// 「プライベートだから無認証」は採らない: scope 付きの client_credentials を要求する（#27）。
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
		Description: "一覧の行ごとに相手を呼ぶ形（HTTP越しのN+1）を避けるためのBatch取得API。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden},
	}, h.listPhotosByGearItem)
}

// ServicePhoto はサービス間で公開する写真の表現。相手のテーブル構造ではなくこの契約に依存させる。
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
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "GetPhotoForService は Phase 4.1 で実装する")
}

func (h *handlers) listPhotosByGearItem(ctx context.Context, in *ListPhotosByGearItemInput) (*ListPhotosByGearItemOutput, error) {
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "ListPhotosByGearItem は Phase 4.1 で実装する")
}
