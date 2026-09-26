// Package readmodel は自ドメインの正から読み取り用の形を組み立てる。
// Entity を経由せず、トランザクションの外で実行する。
package readmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/readmodel/internal/sqlcgen"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// PhotoReader は photos テーブルから Read Model を組み立てる。
type PhotoReader struct {
	q *sqlcgen.Queries
}

// NewPhotoReader は PhotoReader を返す。
func NewPhotoReader(db *sql.DB) *PhotoReader {
	return &PhotoReader{q: sqlcgen.New(db)}
}

// Detail は1件を返す。
func (r *PhotoReader) Detail(ctx context.Context, id domain.PhotoID) (usecase.PhotoView, error) {
	row, err := r.q.GetPhotoDetail(ctx, uint64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return usecase.PhotoView{}, usecase.ErrNotFound
	}
	if err != nil {
		return usecase.PhotoView{}, fmt.Errorf("readmodel: detail: %w", err)
	}
	return view(row), nil
}

// ListByIDs は ID 群に一致する写真を返す。認可付き一覧の WHERE IN に使う。
func (r *PhotoReader) ListByIDs(ctx context.Context, ids []int64, limit int) ([]usecase.PhotoView, error) {
	u := make([]uint64, 0, len(ids))
	for _, id := range ids {
		u = append(u, uint64(id))
	}
	rows, err := r.q.ListPhotosByIDs(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("readmodel: list by ids: %w", err)
	}
	return views(rows, limit), nil
}

// ListByOwner は所有者の写真を返す。
func (r *PhotoReader) ListByOwner(ctx context.Context, ownerSubject string, limit int) ([]usecase.PhotoView, error) {
	rows, err := r.q.ListPhotosByOwner(ctx, sqlcgen.ListPhotosByOwnerParams{
		OwnerSubject: ownerSubject, Limit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("readmodel: list by owner: %w", err)
	}
	return views(rows, limit), nil
}

// ListAll は全件を返す。オペレータ向けの経路からのみ呼ぶ。
func (r *PhotoReader) ListAll(ctx context.Context, limit int) ([]usecase.PhotoView, error) {
	rows, err := r.q.ListPhotos(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("readmodel: list all: %w", err)
	}
	return views(rows, limit), nil
}

func views(rows []sqlcgen.Photo, limit int) []usecase.PhotoView {
	out := make([]usecase.PhotoView, 0, len(rows))
	for i, row := range rows {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, view(row))
	}
	return out
}

func view(row sqlcgen.Photo) usecase.PhotoView {
	v := usecase.PhotoView{
		ID:         int64(row.ID),
		OwnerID:    row.OwnerSubject,
		Caption:    row.Title.String,
		Visibility: string(row.Visibility),
		Status:     string(row.Status),
		ObjectKey:  row.ObjectKey.String,
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
	}
	if row.SizeBytes.Valid {
		v.SizeBytes = row.SizeBytes.Int64
	}
	if row.GearItemID.Valid {
		id := row.GearItemID.Int64
		v.GearItemID = &id
	}
	return v
}

// ListPublicByGearItem は機材に紐づく公開済みの写真を返す。
func (r *PhotoReader) ListPublicByGearItem(ctx context.Context, gearItemID int64, limit int) ([]usecase.PhotoView, error) {
	rows, err := r.q.ListPublicPhotosByGearItem(ctx, sqlcgen.ListPublicPhotosByGearItemParams{
		GearItemID: sql.NullInt64{Int64: gearItemID, Valid: true},
		Limit:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("readmodel: list by gear item: %w", err)
	}
	return views(rows, limit), nil
}
