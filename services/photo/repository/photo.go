// Package repository は usecase の Repository interface を sqlc で実装する。
// 生成型はこのパッケージの internal に閉じており、外の層からは参照できない。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/repository/internal/sqlcgen"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// PhotoRepository は photos テーブルへの読み書き。
type PhotoRepository struct {
	q *sqlcgen.Queries
}

// NewPhotoRepository は PhotoRepository を返す。
func NewPhotoRepository(db *sql.DB) *PhotoRepository {
	return &PhotoRepository{q: sqlcgen.New(db)}
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
		Caption:      string(p.Caption()),
		Visibility:   sqlcgen.PhotosVisibility(p.Visibility()),
		GearItemID:   nullInt64(p.GearItemID()),
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
	return domain.Restore(
		domain.PhotoID(row.ID), row.OwnerSubject, domain.Caption(row.Caption),
		domain.Visibility(row.Visibility), fromNullInt64(row.GearItemID), row.CreatedAt,
	), nil
}

// Save は Entity の状態を行へ書き戻す。
func (r *PhotoRepository) Save(ctx context.Context, p *domain.Photo) error {
	if err := r.queries(ctx).UpdatePhoto(ctx, sqlcgen.UpdatePhotoParams{
		Caption:    string(p.Caption()),
		Visibility: sqlcgen.PhotosVisibility(p.Visibility()),
		GearItemID: nullInt64(p.GearItemID()),
		ID:         uint64(p.ID()),
	}); err != nil {
		return fmt.Errorf("repository: save photo: %w", err)
	}
	return nil
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
