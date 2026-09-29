// Package repository は usecase の ItemRepository を sqlc で実装する。
// 生成型はこのパッケージの internal に閉じており、外の層からは参照できない。
package repository

import (
	"context"
	"database/sql"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/gear/domain"
	"github.com/rikukaInoue/greenfield/services/gear/repository/internal/sqlcgen"
)

// ItemRepository は items テーブルへの読み書き。
type ItemRepository struct {
	q    *sqlcgen.Queries
	conn *sql.DB
}

// NewItemRepository は ItemRepository を返す。
func NewItemRepository(conn *sql.DB) *ItemRepository {
	return &ItemRepository{q: sqlcgen.New(conn), conn: conn}
}

// queries は ctx にトランザクションがあればそれに参加する。
func (r *ItemRepository) queries(ctx context.Context) *sqlcgen.Queries {
	if tx, ok := consistency.TxFrom(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}

// Create は機材を挿入し、確定した ID を Entity へ書き戻す。
func (r *ItemRepository) Create(ctx context.Context, item *domain.GearItem) error {
	res, err := r.queries(ctx).CreateItem(ctx, sqlcgen.CreateItemParams{
		Kind: item.Kind(), Name: item.Name(), Maker: item.Maker(), CreatedBy: item.CreatedBy(),
	})
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	item.SetID(id)
	return nil
}
