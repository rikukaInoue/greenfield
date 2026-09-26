// Package repository は usecase の Repository interface を sqlc で実装する。
// 生成型はこのパッケージの internal に閉じており、外の層からは参照できない。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/repository/internal/sqlcgen"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// PhotoRepository は photos テーブルへの読み書き。
type PhotoRepository struct {
	q    *sqlcgen.Queries
	conn *sql.DB
}

// NewPhotoRepository は PhotoRepository を返す。
func NewPhotoRepository(conn *sql.DB) *PhotoRepository {
	return &PhotoRepository{q: sqlcgen.New(conn), conn: conn}
}

// db は生のクエリを投げる先。ctx にトランザクションがあればそれを使う。
func (r *PhotoRepository) db(ctx context.Context) interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
} {
	if tx, ok := consistency.TxFrom(ctx); ok {
		return tx
	}
	return r.conn
}

// queries は ctx にトランザクションがあればそれに参加する。
func (r *PhotoRepository) queries(ctx context.Context) *sqlcgen.Queries {
	if tx, ok := consistency.TxFrom(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}

// Create は写真を挿入し、確定した ID を Entity へ書き戻す。
func (r *PhotoRepository) Create(ctx context.Context, p *domain.Photo) error {
	res, err := r.queries(ctx).CreatePhoto(ctx, sqlcgen.CreatePhotoParams{
		OwnerSubject: p.OwnerSubject(),
		Title:        nullString(string(p.Caption())),
		Visibility:   sqlcgen.PhotosVisibility(p.Visibility()),
		GearItemID:   nullInt64(p.GearItemID()),
		ObjectKey:    nullString(p.ObjectKey()),
		ContentType:  nullString(p.ContentType()),
		Status:       sqlcgen.PhotosStatus(p.Status()),
	})
	if err != nil {
		return fmt.Errorf("repository: create photo: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("repository: last insert id: %w", err)
	}
	p.AssignID(domain.PhotoID(id))
	return nil
}

// Get は写真を取得する。呼び出しがトランザクション内なら行をロックする。
func (r *PhotoRepository) Get(ctx context.Context, id domain.PhotoID) (*domain.Photo, error) {
	row, err := r.queries(ctx).GetPhotoForUpdate(ctx, uint64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, usecase.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: get photo: %w", err)
	}
	return restore(row), nil
}

func restore(row sqlcgen.Photo) *domain.Photo {
	return domain.Restore(domain.Restored{
		ID:           domain.PhotoID(row.ID),
		OwnerSubject: row.OwnerSubject,
		Caption:      domain.Caption(row.Title.String),
		Visibility:   domain.Visibility(row.Visibility),
		GearItemID:   fromNullInt64(row.GearItemID),
		ObjectKey:    row.ObjectKey.String,
		ContentType:  row.ContentType.String,
		SizeBytes:    fromNullInt64(row.SizeBytes),
		Status:       domain.Status(row.Status),
		CreatedAt:    row.CreatedAt,
	})
}

// Save は Entity の状態を行へ書き戻す。
func (r *PhotoRepository) Save(ctx context.Context, p *domain.Photo) error {
	if err := r.queries(ctx).UpdatePhoto(ctx, sqlcgen.UpdatePhotoParams{
		Title:       nullString(string(p.Caption())),
		Visibility:  sqlcgen.PhotosVisibility(p.Visibility()),
		GearItemID:  nullInt64(p.GearItemID()),
		ContentType: nullString(p.ContentType()),
		SizeBytes:   nullInt64(p.SizeBytes()),
		Status:      sqlcgen.PhotosStatus(p.Status()),
		ID:          uint64(p.ID()),
	}); err != nil {
		return fmt.Errorf("repository: save photo: %w", err)
	}
	return nil
}

// Delete は1件削除する。
func (r *PhotoRepository) Delete(ctx context.Context, id domain.PhotoID) error {
	if err := r.queries(ctx).DeletePhoto(ctx, uint64(id)); err != nil {
		return fmt.Errorf("repository: delete photo: %w", err)
	}
	return nil
}

// ListStalePending はアップロードが完了しないまま放置された写真を古い順に返す。
func (r *PhotoRepository) ListStalePending(ctx context.Context, before time.Time, limit int) ([]*domain.Photo, error) {
	rows, err := r.queries(ctx).ListStalePendingPhotos(ctx, sqlcgen.ListStalePendingPhotosParams{
		CreatedAt: before, Limit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("repository: list stale pending: %w", err)
	}
	out := make([]*domain.Photo, 0, len(rows))
	for _, row := range rows {
		out = append(out, restore(row))
	}
	return out, nil
}

// DeleteByOwner は所有者の写真を全て削除し、件数を返す。
func (r *PhotoRepository) DeleteByOwner(ctx context.Context, ownerSubject string) (int, error) {
	res, err := r.queries(ctx).DeletePhotosByOwner(ctx, ownerSubject)
	if err != nil {
		return 0, fmt.Errorf("repository: delete photos: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("repository: rows affected: %w", err)
	}
	return int(n), nil
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func fromNullInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
