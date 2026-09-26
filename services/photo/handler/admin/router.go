// Package admin は admin リスナー（社内オペレータ）のハンドラ。
package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/httpapi"
	"github.com/rikukaInoue/greenfield/core/problem"
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

// Register は admin リスナーのルートを登録する。
func Register(api httpapi.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api.Huma, huma.Operation{
		OperationID: "AdminListPhotos",
		Method:      http.MethodGet,
		Path:        "/photos",
		Summary:     "全ユーザーの写真一覧（オペレータ）",
		Description: "platform operator の権限で列挙する。",
		Tags:        []string{"photos"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden},
	}, h.adminListPhotos)

	huma.Register(api.Huma, huma.Operation{
		OperationID: "AdminDeleteAccount",
		Method:      http.MethodPost,
		Path:        "/accounts/{subject}:delete",
		Summary:     "アカウントと全投稿を削除する（危険操作）",
		Description: "ステップアップ（RFC 9470）の検証対象。AAL2 を要求する呼び出し語彙を固定してある。",
		Tags:        []string{"accounts"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, h.adminDeleteAccount)
}

type AdminListPhotosInput struct {
	OwnerID string `query:"owner_id,omitempty" doc:"投稿者のSubjectで絞る"`
	Limit   int    `query:"limit,omitempty" minimum:"1" maximum:"200" default:"50" doc:"取得件数"`
}

type AdminListPhotosOutput struct {
	Body struct {
		Photos []AdminPhoto `json:"photos" doc:"写真の一覧"`
	}
}

// AdminPhoto は管理用の写真表現。external の Photo とは別型にして、
// 管理APIのフィールドが外部公開のクライアントへ混ざらないようにする。
type AdminPhoto struct {
	ID         int64  `json:"id" example:"1" doc:"写真ID"`
	OwnerID    string `json:"owner_id" example:"u_01H..." doc:"投稿者のSubject"`
	Caption    string `json:"caption" doc:"キャプション"`
	Visibility string `json:"visibility" enum:"private,public" doc:"公開状態"`
	Status     string `json:"status" enum:"pending_upload,ready" doc:"画像のアップロード状態"`
	CreatedAt  string `json:"created_at" format:"date-time" doc:"投稿時刻"`
}

type AdminDeleteAccountInput struct {
	Subject string `path:"subject" maxLength:"255" doc:"削除対象のSubject"`
}

type AdminDeleteAccountOutput struct {
	Body struct {
		DeletedPhotos int `json:"deleted_photos" doc:"削除した写真の件数"`
	}
}

func (h *handlers) adminListPhotos(ctx context.Context, in *AdminListPhotosInput) (*AdminListPhotosOutput, error) {
	// オペレータ権限は専用機構ではなく platform operator のタプルで判定する。
	if err := h.requireOperator(ctx); err != nil {
		return nil, err
	}
	views, err := h.deps.Queries.ListForOperator(ctx, in.OwnerID, in.Limit)
	if err != nil {
		return nil, problem.New(http.StatusInternalServerError, problem.CodeInternal, "内部エラー")
	}
	out := &AdminListPhotosOutput{}
	out.Body.Photos = make([]AdminPhoto, 0, len(views))
	for _, v := range views {
		out.Body.Photos = append(out.Body.Photos, AdminPhoto{
			ID: v.ID, OwnerID: v.OwnerID, Caption: v.Caption,
			Visibility: v.Visibility, Status: v.Status, CreatedAt: v.CreatedAt,
		})
	}
	return out, nil
}

func (h *handlers) adminDeleteAccount(ctx context.Context, in *AdminDeleteAccountInput) (*AdminDeleteAccountOutput, error) {
	// 保証レベルの要求はミドルウェアのパスマッピングではなくハンドラに明示する。
	if err := h.deps.Assurance.RequireAAL(ctx, authz.AAL2); err != nil {
		return nil, err
	}
	if err := h.requireOperator(ctx); err != nil {
		return nil, err
	}
	n, err := h.deps.Commands.DeleteByOwner(ctx, in.Subject)
	if err != nil {
		return nil, problem.New(http.StatusInternalServerError, problem.CodeInternal, "内部エラー")
	}
	out := &AdminDeleteAccountOutput{}
	out.Body.DeletedPhotos = n
	return out, nil
}

// requireOperator は platform operator のタプルを持つ主体だけを通す。
func (h *handlers) requireOperator(ctx context.Context) error {
	res, err := h.deps.Queries.CanOperate(ctx)
	if err != nil {
		return problem.New(http.StatusInternalServerError, problem.CodeInternal, "内部エラー")
	}
	if !res {
		return problem.New(http.StatusForbidden, problem.CodeForbidden, "オペレータ権限が必要")
	}
	return nil
}
