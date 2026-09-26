// Package external は external リスナー（:8080、一般ユーザー（Authorization Code + PKCE））のハンドラ。
// 呼び出し主体ごとにリスナーを分けるのは、要求するAAL・レート制限・監査・到達経路が異なるため
// （conventions/api-design.md §3.2）。パスプレフィックスによる分離は採らない。
package external

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

// Register は external リスナーのルートを登録する。
// エンドポイントは CQS をそのまま反映する: コマンドはユースケース単位（:verb / 名詞サブリソース）、
// クエリは Read Model 単位の GET。OperationID は対応する usecase 名と一致させる（生成クライアントのメソッド名になる）。
func Register(api httpapi.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api.Huma, huma.Operation{
		OperationID: "CreatePhoto",
		Method:      http.MethodPost,
		Path:        "/photos",
		Summary:     "写真を投稿する",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity},
	}, h.createPhoto)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "PublishPhoto",
		Method:      http.MethodPost,
		Path:        "/photos/{id}:publish",
		Summary:     "写真を公開する",
		Description: "純粋な状態遷移のため :verb（実体を生む操作は名詞のサブリソースにする）。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.publishPhoto)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "GetPhotoDetail",
		Method:      http.MethodGet,
		Path:        "/photos/{id}",
		Summary:     "写真の詳細（Read Model）",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, h.getPhotoDetail)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "ListPhotos",
		Method:      http.MethodGet,
		Path:        "/photos",
		Summary:     "写真の一覧（Read Model）",
		Description: "ListAccessible で得たIDを WHERE IN で絞るため、他人の写真は不可視。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized},
	}, h.listPhotos)
}

// Photo は写真の表現（Read Model → 応答 DTO）。
type Photo struct {
	ID         int64   `json:"id" example:"1" doc:"写真ID"`
	OwnerID    string  `json:"owner_id" example:"u_01H..." doc:"投稿者のSubject"`
	Caption    string  `json:"caption" example:"朝の光" doc:"キャプション"`
	Visibility string  `json:"visibility" enum:"private,public" example:"public" doc:"公開状態"`
	GearItemID *int64  `json:"gear_item_id,omitempty" example:"42" doc:"使用機材（gear の item ID）"`
	GearName   *string `json:"gear_name,omitempty" example:"X-T5" doc:"使用機材の表示名（ReplicaView 由来。業務判断に使わない）"`
	CreatedAt  string  `json:"created_at" format:"date-time" doc:"投稿時刻"`
}

// CreatePhotoInput はコマンドの入力。形式的な入力検証は huma の入力型が担う
// （業務上の不変条件・状態遷移は Entity が保証する）。
type CreatePhotoInput struct {
	Body struct {
		Caption    string `json:"caption" maxLength:"1000" doc:"キャプション"`
		Visibility string `json:"visibility,omitempty" enum:"private,public" default:"private" doc:"公開状態"`
		GearItemID *int64 `json:"gear_item_id,omitempty" minimum:"1" doc:"使用機材（gear の item ID）。指定すると gear への紐付けが pending で始まる"`
	}
}

type CreatePhotoOutput struct {
	Status int
	Body   Photo
}

type PublishPhotoInput struct {
	ID int64 `path:"id" minimum:"1" doc:"写真ID"`
}

type PublishPhotoOutput struct {
	Body Photo
}

type GetPhotoDetailInput struct {
	ID int64 `path:"id" minimum:"1" doc:"写真ID"`
}

type GetPhotoDetailOutput struct {
	Body Photo
}

type ListPhotosInput struct {
	Visibility string `query:"visibility,omitempty" enum:"private,public" doc:"公開状態で絞る"`
	Limit      int    `query:"limit,omitempty" minimum:"1" maximum:"100" default:"50" doc:"取得件数"`
}

type ListPhotosOutput struct {
	Body struct {
		Photos []Photo `json:"photos" doc:"写真の一覧"`
	}
}

// 以下のハンドラは 0.4 時点では契約（OpenAPI）を確定させるための骨格であり、
// 実装は Phase 1（1.1 Atomic / 1.2 CQS / 1.3 認可呼び出し）で入れる。
// usecase を呼ぶ形（handler → usecase → Entity）は最初から固定しておく。

func (h *handlers) createPhoto(ctx context.Context, in *CreatePhotoInput) (*CreatePhotoOutput, error) {
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "CreatePhoto は Phase 1.1 で実装する")
}

func (h *handlers) publishPhoto(ctx context.Context, in *PublishPhotoInput) (*PublishPhotoOutput, error) {
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "PublishPhoto は Phase 1.1 で実装する")
}

func (h *handlers) getPhotoDetail(ctx context.Context, in *GetPhotoDetailInput) (*GetPhotoDetailOutput, error) {
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "GetPhotoDetail は Phase 1.2 で実装する")
}

func (h *handlers) listPhotos(ctx context.Context, in *ListPhotosInput) (*ListPhotosOutput, error) {
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "ListPhotos は Phase 1.2 で実装する")
}
