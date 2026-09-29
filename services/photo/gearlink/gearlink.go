// Package gearlink は gear への同期コマンド「使用機材の紐付け」のアダプタ。
// 生成クライアント（gear-client）を usecase.GearLink interface に合わせて包み、
// **生成型を usecase に見せない**（境界での型変換。4.1 の photocatalog と同じ流儀）。
package gearlink

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	gearclient "github.com/rikukaInoue/greenfield/services/gear-client"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// Client は usecase.GearLink の実装。
type Client struct {
	c *gearclient.Client
}

// New は gear internal のベースURLと M2M 付き http.Client からアダプタを組む。
func New(baseURL string, hc *http.Client) (*Client, error) {
	c, err := gearclient.NewClient(baseURL, gearclient.WithHTTPClient(hc))
	if err != nil {
		return nil, fmt.Errorf("gearlink: %w", err)
	}
	return &Client{c: c}, nil
}

// Link は紐付けコマンドを送る。key はヘッダの Idempotency-Key として渡り、
// core/httpclient の自動付与は**呼び出し側のキーを尊重**するのでこの値が生きる（C-7）。
func (g *Client) Link(ctx context.Context, itemID, photoID int64, key string) (usecase.GearLinkResult, error) {
	res, err := g.c.LinkPhoto(ctx, itemID,
		&gearclient.LinkPhotoParams{IdempotencyKey: key},
		gearclient.LinkPhotoJSONRequestBody{PhotoId: photoID})
	if err != nil {
		return usecase.GearLinkResult{}, fmt.Errorf("gearlink: link: %w", err)
	}
	return decode(res)
}

// Get は受理記録をキーで照会する。404 は「届いていない」= ErrLinkNotFound。
func (g *Client) Get(ctx context.Context, key string) (usecase.GearLinkResult, error) {
	res, err := g.c.GetPhotoLink(ctx, key)
	if err != nil {
		return usecase.GearLinkResult{}, fmt.Errorf("gearlink: get: %w", err)
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return usecase.GearLinkResult{}, usecase.ErrLinkNotFound
	}
	return decode(res)
}

func decode(res *http.Response) (usecase.GearLinkResult, error) {
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return usecase.GearLinkResult{}, fmt.Errorf("gearlink: read: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return usecase.GearLinkResult{}, fmt.Errorf("gearlink: %d: %s", res.StatusCode, body)
	}
	var link gearclient.Link
	if err := json.Unmarshal(body, &link); err != nil {
		return usecase.GearLinkResult{}, fmt.Errorf("gearlink: decode: %w", err)
	}
	out := usecase.GearLinkResult{Status: string(link.Status)}
	if link.Reason != nil {
		out.Reason = *link.Reason
	}
	return out, nil
}
