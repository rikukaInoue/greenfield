// Package external は external リスナー（:8090、一般ユーザー（Authorization Code + PKCE））のハンドラ。
// 呼び出し主体ごとにリスナーを分けるのは、要求するAAL・レート制限・監査・到達経路が異なるため
// （conventions/api-design.md §3.2）。パスプレフィックスによる分離は採らない。
package external

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/rikukaInoue/greenfield/core/problem"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// Deps はハンドラが使う差し込み口。実装は app/ が注入する。
// ハンドラは interface しか見ないため、本番アダプタへの差し替えで本ファイルは変わらない（#18）。
type Deps struct {
	Commands *usecase.ItemCommands
	Queries  *usecase.ItemQueries
}

type handlers struct {
	deps Deps
}

// Register は external リスナーのルートを登録する。
func Register(api huma.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api, huma.Operation{
		OperationID:   "CreateItem",
		Method:        http.MethodPost,
		Path:          "/items",
		DefaultStatus: http.StatusCreated,
		Summary:       "機材を投稿する",
		Description:   "カタログは共有財。認証済みであれば誰でも投稿できる。",
		Tags:          []string{"items"},
		Errors:        []int{http.StatusUnauthorized, http.StatusUnprocessableEntity},
	}, h.createItem)

	huma.Register(api, huma.Operation{
		OperationID: "GetItemDetail",
		Method:      http.MethodGet,
		Path:        "/items/{id}",
		Summary:     "機材の詳細と作例",
		Description: "作例は photo サービスの internal API から M2M で引く。取得に失敗したら詳細ごと失敗にする（半端な応答を作らない）。",
		Tags:        []string{"items"},
		Errors:      []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusBadGateway},
	}, h.getItemDetail)

	huma.Register(api, huma.Operation{
		OperationID: "ListItems",
		Method:      http.MethodGet,
		Path:        "/items",
		Summary:     "機材の一覧",
		Tags:        []string{"items"},
		Errors:      []int{http.StatusUnauthorized},
	}, h.listItems)
}

// Item は機材の応答表現。
type Item struct {
	ID        int64  `json:"id" example:"1" doc:"機材ID"`
	Kind      string `json:"kind" example:"camera" doc:"分類"`
	Name      string `json:"name" example:"X-T5" doc:"機種名"`
	Maker     string `json:"maker,omitempty" example:"FUJIFILM" doc:"メーカー名"`
	CreatedAt string `json:"created_at,omitempty" format:"date-time" doc:"投稿時刻"`
}

// PhotoRef は作例の参照（photo の公開情報の写し。表示専用）。
type PhotoRef struct {
	ID       int64  `json:"id" doc:"写真ID（photo サービスの ID）"`
	Caption  string `json:"caption" doc:"キャプション"`
	ImageURL string `json:"image_url,omitempty" doc:"画像取得用の署名付きURL。期限付き"`
}

type CreateItemInput struct {
	Body struct {
		Kind  string `json:"kind" enum:"camera,lens,tripod,filter,bag,accessory" doc:"分類"`
		Name  string `json:"name" minLength:"1" maxLength:"120" doc:"機種名"`
		Maker string `json:"maker,omitempty" maxLength:"120" doc:"メーカー名"`
	}
}

type CreateItemOutput struct {
	Body Item
}

type GetItemDetailInput struct {
	ID int64 `path:"id" minimum:"1" doc:"機材ID"`
}

type GetItemDetailOutput struct {
	Body struct {
		Item
		Photos []PhotoRef `json:"photos" doc:"作例（公開済みの写真）"`
	}
}

type ListItemsInput struct {
	Limit int `query:"limit,omitempty" minimum:"1" maximum:"100" default:"50" doc:"取得件数"`
}

// ListedItem は一覧の行。作例の件数は ReplicaView 由来（結果整合の窓あり、表示専用）。
type ListedItem struct {
	Item
	PhotoCount int64 `json:"photo_count" doc:"作例の件数（反映に遅延がありうる）"`
}

type ListItemsOutput struct {
	Body struct {
		Items []ListedItem `json:"items" doc:"機材の一覧"`
	}
}

func (h *handlers) createItem(ctx context.Context, in *CreateItemInput) (*CreateItemOutput, error) {
	item, err := h.deps.Commands.Create(ctx, usecase.CreateItemInput{
		Kind: in.Body.Kind, Name: in.Body.Name, Maker: in.Body.Maker,
	})
	if err != nil {
		return nil, toHTTP(ctx, err)
	}
	return &CreateItemOutput{Body: Item{
		ID: item.ID(), Kind: item.Kind(), Name: item.Name(), Maker: item.Maker(),
	}}, nil
}

func (h *handlers) getItemDetail(ctx context.Context, in *GetItemDetailInput) (*GetItemDetailOutput, error) {
	d, err := h.deps.Queries.Detail(ctx, in.ID)
	if err != nil {
		return nil, toHTTP(ctx, err)
	}
	out := &GetItemDetailOutput{}
	out.Body.Item = fromView(d.ItemView)
	out.Body.Photos = make([]PhotoRef, 0, len(d.Photos))
	for _, p := range d.Photos {
		out.Body.Photos = append(out.Body.Photos, PhotoRef(p))
	}
	return out, nil
}

func (h *handlers) listItems(ctx context.Context, in *ListItemsInput) (*ListItemsOutput, error) {
	views, err := h.deps.Queries.List(ctx, in.Limit)
	if err != nil {
		return nil, toHTTP(ctx, err)
	}
	out := &ListItemsOutput{}
	out.Body.Items = make([]ListedItem, 0, len(views))
	for _, v := range views {
		out.Body.Items = append(out.Body.Items, ListedItem{Item: fromView(v), PhotoCount: v.PhotoCount})
	}
	return out, nil
}

func fromView(v usecase.ItemView) Item {
	return Item{ID: v.ID, Kind: v.Kind, Name: v.Name, Maker: v.Maker, CreatedAt: v.Created}
}

func toHTTP(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, usecase.ErrNotFound):
		return problem.New(http.StatusNotFound, "gear.not_found", "機材が見つからない")
	case errors.Is(err, usecase.ErrInvalid):
		return problem.New(http.StatusUnprocessableEntity, "gear.invalid", err.Error())
	default:
		// 原因（接続先URL等の内部情報を含む）は応答に載せずログへ。
		// AccessLog はステータスしか持たないので、原因はここで落とすと消える
		slog.ErrorContext(ctx, "作例の取得に失敗", "error", err)
		return problem.New(http.StatusBadGateway, "gear.upstream", "作例の取得に失敗した")
	}
}
