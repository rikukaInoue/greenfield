// Package photocatalog は usecase.PhotoCatalog を photo-client（生成コード）で実装する。
//
// これが「生成→配布→interface受け」の受け側（4.1 の主眼）。photo の internal スペックから
// oapi-codegen が生成したクライアントを、ここで gear の語彙（usecase.PhotoRef）へ変換する。
// 生成型（ServicePhoto 等）はこのパッケージから外に出さない——スペックの変更が
// usecase / handler へ波及しないための境界。
//
// 認証は M2M（svc-gear の client_credentials、scope internal:photo）。トークンの取得・
// キャッシュ・自動付与と Idempotency-Key・traceparent は core/httpclient が担う。
package photocatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	photoclient "github.com/rikukaInoue/greenfield/services/photo-client"

	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// Catalog は usecase.PhotoCatalog の実装。
type Catalog struct {
	c *photoclient.Client
}

// New は photo の internal リスナー（例: http://localhost:8081）へのカタログを返す。
// hc には core/httpclient の M2M トークン付きクライアントを渡す。
func New(baseURL string, hc *http.Client) (*Catalog, error) {
	c, err := photoclient.NewClient(baseURL, photoclient.WithHTTPClient(hc))
	if err != nil {
		return nil, err
	}
	return &Catalog{c: c}, nil
}

// PublicPhotosByItem は機材に紐づく公開済みの作例を返す。
func (p *Catalog) PublicPhotosByItem(ctx context.Context, itemID int64, limit int) ([]usecase.PhotoRef, error) {
	l := int64(limit)
	res, err := p.c.ListPhotosByGearItem(ctx, itemID, &photoclient.ListPhotosByGearItemParams{Limit: &l})
	if err != nil {
		return nil, fmt.Errorf("photocatalog: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photocatalog: photo internal が %d を返した", res.StatusCode)
	}
	var body photoclient.ListPhotosByGearItemOutputBody
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("photocatalog: 応答を読めない: %w", err)
	}
	// 境界での型変換: 生成型 → gear の語彙。ここより内側に生成型は出さない
	var photos []photoclient.ServicePhoto
	if body.Photos != nil {
		photos = *body.Photos
	}
	out := make([]usecase.PhotoRef, 0, len(photos))
	for _, ph := range photos {
		ref := usecase.PhotoRef{ID: ph.Id, Caption: ph.Caption}
		if ph.ImageUrl != nil {
			ref.ImageURL = *ph.ImageUrl
		}
		out = append(out, ref)
	}
	return out, nil
}
