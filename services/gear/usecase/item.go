// Package usecase は gear のユースケース。Repository / 外部サービスは interface で受け、
// 実装（sqlc / photo-client）は app が注入する。
package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/services/gear/domain"
)

// ErrNotFound は対象が存在しないことを表す。
var ErrNotFound = errors.New("gear: 見つからない")

// ErrInvalid は入力の検証エラー。
var ErrInvalid = errors.New("gear: 入力が不正")

// ItemRepository は items テーブルへの読み書き。
type ItemRepository interface {
	Create(ctx context.Context, item *domain.GearItem) error
}

// ItemReader は Read Model（一覧・詳細）。
type ItemReader interface {
	Detail(ctx context.Context, id int64) (ItemView, error)
	List(ctx context.Context, limit int) ([]ItemView, error)
}

// PhotoCatalog は photo サービスから作例を引く差し込み口。
// 実装（photo-client の生成コード + M2M トークン）は app が注入する。
// **境界での型変換はアダプタが担い、usecase には生成型を持ち込まない**（4.1 の主眼）。
type PhotoCatalog interface {
	PublicPhotosByItem(ctx context.Context, itemID int64, limit int) ([]PhotoRef, error)
}

// PhotoRef は作例（photo の公開情報の写し）。表示専用であり業務判断に使わない。
type PhotoRef struct {
	ID       int64
	Caption  string
	ImageURL string
}

// ItemView は機材の読み取りビュー。
type ItemView struct {
	ID      int64
	Kind    string
	Name    string
	Maker   string
	Created string
}

// ItemDetail は詳細 + 作例。
type ItemDetail struct {
	ItemView
	Photos []PhotoRef
}

// ItemCommands はコマンド側。
type ItemCommands struct {
	atomic Atomic
	items  ItemRepository
}

// NewItemCommands はコマンド一式を組む。
func NewItemCommands(atomic Atomic, items ItemRepository) *ItemCommands {
	return &ItemCommands{atomic: atomic, items: items}
}

// CreateItemInput は投稿の入力。
type CreateItemInput struct {
	Kind  string
	Name  string
	Maker string
}

// Create は機材を投稿する。認証済みであれば誰でも投稿できる（カタログは共有財。
// 所有者 ReBAC が要る編集・削除はこの検証ビルドのスコープ外で、必要になったら
// photo と同型のモデルを足す）。
func (c *ItemCommands) Create(ctx context.Context, in CreateItemInput) (*domain.GearItem, error) {
	p, ok := authz.PrincipalFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("%w: 未認証", ErrInvalid)
	}
	item, err := domain.NewGearItem(p.Subject, in.Kind, in.Name, in.Maker)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	// Atomic 判定: items への INSERT 1件のみ（ローカルDBのみ、外部書き込みなし）
	if err := c.atomic.Do(ctx, func(ctx context.Context) error {
		return c.items.Create(ctx, item)
	}); err != nil {
		return nil, err
	}
	return item, nil
}

// ItemQueries はクエリ側。
type ItemQueries struct {
	reader ItemReader
	photos PhotoCatalog
}

// NewItemQueries はクエリ一式を組む。
func NewItemQueries(reader ItemReader, photos PhotoCatalog) *ItemQueries {
	return &ItemQueries{reader: reader, photos: photos}
}

// detailPhotoLimit は詳細に添える作例の数。
const detailPhotoLimit = 12

// Detail は機材の詳細と作例を返す。作例の取得失敗は詳細ごと失敗にする
// （半端に photos だけ欠けた応答は、呼び出し側に「空」と「取れなかった」の
// 区別を失わせる。監査 B-1 と同じ形を API 応答で作らない）。
func (q *ItemQueries) Detail(ctx context.Context, id int64) (ItemDetail, error) {
	v, err := q.reader.Detail(ctx, id)
	if err != nil {
		return ItemDetail{}, err
	}
	photos, err := q.photos.PublicPhotosByItem(ctx, id, detailPhotoLimit)
	if err != nil {
		return ItemDetail{}, fmt.Errorf("作例の取得: %w", err)
	}
	return ItemDetail{ItemView: v, Photos: photos}, nil
}

// List は機材の一覧。
func (q *ItemQueries) List(ctx context.Context, limit int) ([]ItemView, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return q.reader.List(ctx, limit)
}
