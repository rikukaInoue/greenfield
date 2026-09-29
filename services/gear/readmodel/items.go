// Package readmodel は一覧・詳細の Read Model。tx の外で素のコネクションから読む。
package readmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rikukaInoue/greenfield/services/gear/readmodel/internal/sqlcgen"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// ItemReader は usecase.ItemReader の実装。
type ItemReader struct {
	q *sqlcgen.Queries
}

// NewItemReader は ItemReader を返す。
func NewItemReader(conn *sql.DB) *ItemReader {
	return &ItemReader{q: sqlcgen.New(conn)}
}

// Detail は機材1件のビュー。
func (r *ItemReader) Detail(ctx context.Context, id int64) (usecase.ItemView, error) {
	row, err := r.q.GetItemDetail(ctx, uint64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return usecase.ItemView{}, usecase.ErrNotFound
	}
	if err != nil {
		return usecase.ItemView{}, fmt.Errorf("readmodel: item detail: %w", err)
	}
	return view(row.ID, row.Kind, row.Name, row.Maker, row.CreatedAt), nil
}

// List は新しい順の一覧。
func (r *ItemReader) List(ctx context.Context, limit int) ([]usecase.ItemView, error) {
	rows, err := r.q.ListItems(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("readmodel: list items: %w", err)
	}
	out := make([]usecase.ItemView, 0, len(rows))
	for _, row := range rows {
		out = append(out, view(row.ID, row.Kind, row.Name, row.Maker, row.CreatedAt))
	}
	return out, nil
}

func view(id uint64, kind, name, maker string, created time.Time) usecase.ItemView {
	return usecase.ItemView{
		ID: int64(id), Kind: kind, Name: name, Maker: maker,
		Created: created.UTC().Format(time.RFC3339),
	}
}
