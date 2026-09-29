package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/gear/repository/internal/sqlcgen"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// LinkRepository は usecase.LinkRepository の実装。
type LinkRepository struct {
	q *sqlcgen.Queries
}

// NewLinkRepository は LinkRepository を返す。
func NewLinkRepository(db *sql.DB) *LinkRepository {
	return &LinkRepository{q: sqlcgen.New(db)}
}

func (r *LinkRepository) queries(ctx context.Context) *sqlcgen.Queries {
	if tx, ok := consistency.TxFrom(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}

// Insert は受理記録を試みる。同じキーが既にあれば inserted=false（INSERT IGNORE。上書きしない）。
func (r *LinkRepository) Insert(ctx context.Context, link usecase.PhotoLink) (bool, error) {
	res, err := r.queries(ctx).InsertPhotoLink(ctx, sqlcgen.InsertPhotoLinkParams{
		LinkKey: link.Key, ItemID: uint64(link.ItemID), PhotoID: uint64(link.PhotoID),
		Status: link.Status, Reason: link.Reason,
	})
	if err != nil {
		return false, fmt.Errorf("repository: photo_link insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("repository: photo_link insert result: %w", err)
	}
	return n > 0, nil
}

// Get はキーで受理記録を引く。
func (r *LinkRepository) Get(ctx context.Context, key string) (usecase.PhotoLink, error) {
	row, err := r.queries(ctx).GetPhotoLink(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return usecase.PhotoLink{}, usecase.ErrNotFound
	}
	if err != nil {
		return usecase.PhotoLink{}, fmt.Errorf("repository: photo_link get: %w", err)
	}
	return usecase.PhotoLink{
		Key: row.LinkKey, ItemID: int64(row.ItemID), PhotoID: int64(row.PhotoID),
		Status: row.Status, Reason: row.Reason,
	}, nil
}

// ItemExists は機材の存在を返す。
func (r *LinkRepository) ItemExists(ctx context.Context, itemID int64) (bool, error) {
	ok, err := r.queries(ctx).ItemExists(ctx, uint64(itemID))
	if err != nil {
		return false, fmt.Errorf("repository: item exists: %w", err)
	}
	return ok, nil
}
