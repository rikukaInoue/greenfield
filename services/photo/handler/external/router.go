// Package external は external リスナー（一般ユーザー）のハンドラ。
package external

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/problem"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// Deps はハンドラが使う依存。実装は app/ が注入する。
type Deps struct {
	Commands  *usecase.PhotoCommands
	Queries   *usecase.PhotoQueries
	Assurance authz.AssuranceChecker
}

type handlers struct {
	deps Deps
}

// Register は external リスナーのルートを登録する。
func Register(api httpapi.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api.Huma, huma.Operation{
		OperationID: "CreatePhoto",
		Method:      http.MethodPost,
		Path:        "/photos",
		Summary:     "写真を投稿する",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, h.createPhoto)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "CommitPhoto",
		Method:      http.MethodPost,
		Path:        "/photos/{id}:commit",
		Summary:     "画像のアップロード完了を確定する",
		Description: "署名URLへの PUT が終わったら呼ぶ。画像の実体が確認できなければ確定しない。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, h.commitPhoto)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "PublishPhoto",
		Method:      http.MethodPost,
		Path:        "/photos/{id}:publish",
		Summary:     "写真を公開する",
		Description: "純粋な状態遷移のため :verb を使う。",
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

// Photo は写真の応答表現。
type Photo struct {
	ID         int64   `json:"id" example:"1" doc:"写真ID"`
	OwnerID    string  `json:"owner_id" example:"u_01H..." doc:"投稿者のSubject"`
	Caption    string  `json:"caption" example:"朝の光" doc:"キャプション"`
	Visibility string  `json:"visibility" enum:"private,public" example:"public" doc:"公開状態"`
	Status     string  `json:"status" enum:"pending_upload,ready" example:"ready" doc:"画像のアップロード状態"`
	GearItemID *int64  `json:"gear_item_id,omitempty" example:"42" doc:"使用機材（gear の item ID）"`
	GearName   *string `json:"gear_name,omitempty" example:"X-T5" doc:"使用機材の表示名（ReplicaView 由来。業務判断に使わない）"`
	ImageURL   string  `json:"image_url,omitempty" doc:"画像取得用の署名付きURL。期限付き"`
	SizeBytes  int64   `json:"size_bytes,omitempty" doc:"画像のバイト数"`
	CreatedAt  string  `json:"created_at" format:"date-time" doc:"投稿時刻"`
}

// CreatePhotoInput は投稿コマンドの入力。形式的な検証はこの型が担う。
type CreatePhotoInput struct {
	Body struct {
		Caption     string `json:"caption" maxLength:"1000" doc:"キャプション"`
		Visibility  string `json:"visibility,omitempty" enum:"private,public" default:"private" doc:"公開状態"`
		GearItemID  *int64 `json:"gear_item_id,omitempty" minimum:"1" doc:"使用機材（gear の item ID）。指定すると gear への紐付けが pending で始まる"`
		ContentType string `json:"content_type" enum:"image/jpeg,image/png,image/webp,image/avif" example:"image/jpeg" doc:"アップロードする画像の種類"`
	}
}

type CreatePhotoOutput struct {
	Body struct {
		Photo
		// UploadURL へ画像を PUT し、その後 :commit を呼ぶ。
		UploadURL       string `json:"upload_url" doc:"画像アップロード用の署名付きURL。Content-Type ヘッダを content_type と同じ値で送る"`
		UploadExpiresAt string `json:"upload_expires_at" format:"date-time" doc:"署名URLの有効期限"`
	}
}

type CommitPhotoInput struct {
	ID int64 `path:"id" minimum:"1" doc:"写真ID"`
}

type CommitPhotoOutput struct {
	Body Photo
}

type PublishPhotoInput struct {
	ID int64 `path:"id" minimum:"1" doc:"写真ID"`
}

type PublishPhotoOutput struct {
	Body Photo
}

type GetPhotoDetailInput struct {
	ID    int64 `path:"id" minimum:"1" doc:"写真ID"`
	Fresh bool  `query:"fresh,omitempty" doc:"作成直後でも確実に見えるよう、認可判定の鮮度を上げる"`
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

func (h *handlers) createPhoto(ctx context.Context, in *CreatePhotoInput) (*CreatePhotoOutput, error) {
	res, err := h.deps.Commands.Create(ctx, usecase.CreatePhotoInput{
		Caption:     in.Body.Caption,
		Visibility:  in.Body.Visibility,
		GearItemID:  in.Body.GearItemID,
		ContentType: in.Body.ContentType,
	})
	if err != nil {
		return nil, toHTTP(err)
	}
	out := &CreatePhotoOutput{}
	out.Body.Photo = fromEntity(res.Photo)
	out.Body.UploadURL = res.Upload.URL
	out.Body.UploadExpiresAt = res.Upload.ExpiresAt.UTC().Format(time.RFC3339)
	return out, nil
}

func (h *handlers) commitPhoto(ctx context.Context, in *CommitPhotoInput) (*CommitPhotoOutput, error) {
	photo, err := h.deps.Commands.CommitUpload(ctx, domain.PhotoID(in.ID))
	if err != nil {
		return nil, toHTTP(err)
	}
	return &CommitPhotoOutput{Body: fromEntity(photo)}, nil
}

func (h *handlers) publishPhoto(ctx context.Context, in *PublishPhotoInput) (*PublishPhotoOutput, error) {
	photo, err := h.deps.Commands.Publish(ctx, domain.PhotoID(in.ID))
	if err != nil {
		return nil, toHTTP(err)
	}
	return &PublishPhotoOutput{Body: fromEntity(photo)}, nil
}

func (h *handlers) getPhotoDetail(ctx context.Context, in *GetPhotoDetailInput) (*GetPhotoDetailOutput, error) {
	consistency := authz.ConsistencyDefault
	if in.Fresh {
		consistency = authz.ConsistencyHigher
	}
	v, err := h.deps.Queries.Detail(ctx, domain.PhotoID(in.ID), consistency)
	if err != nil {
		return nil, toHTTP(err)
	}
	return &GetPhotoDetailOutput{Body: fromView(v)}, nil
}

func (h *handlers) listPhotos(ctx context.Context, in *ListPhotosInput) (*ListPhotosOutput, error) {
	views, err := h.deps.Queries.List(ctx, in.Limit)
	if err != nil {
		return nil, toHTTP(err)
	}
	out := &ListPhotosOutput{}
	out.Body.Photos = make([]Photo, 0, len(views))
	for _, v := range views {
		if in.Visibility != "" && v.Visibility != in.Visibility {
			continue
		}
		out.Body.Photos = append(out.Body.Photos, fromView(v))
	}
	return out, nil
}

func fromEntity(p *domain.Photo) Photo {
	out := Photo{
		ID:         int64(p.ID()),
		OwnerID:    p.OwnerSubject(),
		Caption:    string(p.Caption()),
		Visibility: string(p.Visibility()),
		Status:     string(p.Status()),
		GearItemID: p.GearItemID(),
	}
	if p.SizeBytes() != nil {
		out.SizeBytes = *p.SizeBytes()
	}
	if !p.CreatedAt().IsZero() {
		out.CreatedAt = p.CreatedAt().UTC().Format(time.RFC3339)
	}
	return out
}

func fromView(v usecase.PhotoView) Photo {
	return Photo{
		ID:         v.ID,
		OwnerID:    v.OwnerID,
		Caption:    v.Caption,
		Visibility: v.Visibility,
		Status:     v.Status,
		GearItemID: v.GearItemID,
		ImageURL:   v.ImageURL,
		SizeBytes:  v.SizeBytes,
		CreatedAt:  v.CreatedAt,
	}
}

// toHTTP はドメイン・ユースケースのエラーを problem+json へ対応づける。
func toHTTP(err error) error {
	switch {
	case errors.Is(err, usecase.ErrNotFound):
		return problem.New(http.StatusNotFound, "photo.not_found", "写真が見つからない")
	case errors.Is(err, usecase.ErrForbidden):
		return problem.New(http.StatusForbidden, problem.CodeForbidden, "権限がない")
	case errors.Is(err, domain.ErrAlreadyPublished):
		return problem.New(http.StatusConflict, "photo.already_published", "すでに公開済み")
	case errors.Is(err, domain.ErrUploadNotFinished):
		return problem.New(http.StatusConflict, "photo.upload_not_finished", "画像のアップロードが完了していない")
	case errors.Is(err, domain.ErrNotPending):
		return problem.New(http.StatusConflict, "photo.not_pending", "アップロード待ちではない")
	case errors.Is(err, usecase.ErrObjectNotFound):
		return problem.New(http.StatusConflict, "photo.object_not_found", "画像がアップロードされていない")
	case errors.Is(err, usecase.ErrUploadsDisabled):
		return problem.New(http.StatusServiceUnavailable, "photo.uploads_disabled", "投稿を一時停止中")
	case errors.Is(err, domain.ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, problem.CodeValidationFailed, err.Error())
	case errors.Is(err, usecase.ErrInjectedFault):
		return problem.New(http.StatusInternalServerError, "photo.injected_fault", "注入された失敗")
	default:
		return problem.New(http.StatusInternalServerError, problem.CodeInternal, "内部エラー")
	}
}
