package usecase_test

import (
	"context"
	"time"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// DB もストレージも使わずに usecase の分岐だけを検証するためのスタブ。

type stubRepo struct {
	created []*domain.Photo
}

func (r *stubRepo) Create(_ context.Context, p *domain.Photo) error {
	p.AssignID(domain.PhotoID(len(r.created) + 1))
	r.created = append(r.created, p)
	return nil
}
func (r *stubRepo) Get(_ context.Context, id domain.PhotoID) (*domain.Photo, error) {
	for _, p := range r.created {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, usecase.ErrNotFound
}
func (r *stubRepo) Save(context.Context, *domain.Photo) error          { return nil }
func (r *stubRepo) Delete(context.Context, domain.PhotoID) error       { return nil }
func (r *stubRepo) DeleteByOwner(context.Context, string) (int, error) { return 0, nil }
func (r *stubRepo) ListStalePending(context.Context, time.Time, int) ([]*domain.Photo, error) {
	return nil, nil
}

type stubImages struct{}

func (stubImages) NewKey(string, string) (string, error) { return "photos/stub.png", nil }
func (stubImages) PresignPut(context.Context, string, string, time.Duration) (usecase.UploadTarget, error) {
	return usecase.UploadTarget{URL: "http://example.test/put", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (stubImages) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "http://example.test/get", nil
}
func (stubImages) Stat(context.Context, string) (usecase.ObjectInfo, error) {
	return usecase.ObjectInfo{SizeBytes: 1024, ContentType: "image/png"}, nil
}
func (stubImages) Delete(context.Context, string) error { return nil }

type stubRelations struct {
	written []authz.Tuple
}

func (r *stubRelations) WriteRelations(_ context.Context, t []authz.Tuple) error {
	r.written = append(r.written, t...)
	return nil
}
func (r *stubRelations) DeleteRelations(context.Context, []authz.Tuple) error { return nil }
