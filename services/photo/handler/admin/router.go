// Package admin は admin リスナー（:8082、社内オペレータ）のハンドラ。
// 呼び出し主体ごとにリスナーを分けるのは、要求するAAL・レート制限・監査・到達経路が異なるため
// （conventions/api-design.md §3.2）。パスプレフィックスによる分離は採らない。
package admin

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
	Assurance authz.AssuranceChecker
}

type handlers struct {
	deps Deps
}

// Register は admin リスナーのルートを登録する。
// オペレータの権限は専用機構を作らず ReBAC（platform operator）に載せる（docs/03-platform.md）。
func Register(api httpapi.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api.Huma, huma.Operation{
		OperationID: "AdminListPhotos",
		Method:      http.MethodGet,
		Path:        "/photos",
		Summary:     "全ユーザーの写真一覧（オペレータ）",
		Description: "platform operator の権限で列挙する。専用機構ではなく ReBAC で解決する。",
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

// AdminPhoto は管理用の写真表現。external の Photo とは別型とし、
// 管理APIのフィールドが外部公開のクライアントへ漏れない形にする（生成クライアントもパッケージが分かれる）。
type AdminPhoto struct {
	ID         int64  `json:"id" example:"1" doc:"写真ID"`
	OwnerID    string `json:"owner_id" example:"u_01H..." doc:"投稿者のSubject"`
	Caption    string `json:"caption" doc:"キャプション"`
	Visibility string `json:"visibility" enum:"private,public" doc:"公開状態"`
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
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "AdminListPhotos は Phase 1.2 で実装する")
}

func (h *handlers) adminDeleteAccount(ctx context.Context, in *AdminDeleteAccountInput) (*AdminDeleteAccountOutput, error) {
	// 保証レベルの要求はミドルウェアのパスマッピングではなくハンドラに明示する
	// （パス設計の変更で認可が静かに壊れる事故を避けるため。conventions/internal-04）。
	if err := h.deps.Assurance.RequireAAL(ctx, authz.AAL2); err != nil {
		return nil, err
	}
	return nil, problem.New(http.StatusNotImplemented, "photo.not_implemented", "AdminDeleteAccount は Phase 5 で実装する")
}
