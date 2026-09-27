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
func (r *stubRepo) Save(context.Context, *domain.Photo) error    { return nil }
func (r *stubRepo) Delete(context.Context, domain.PhotoID) error { return nil }
func (r *stubRepo) DeleteByOwner(_ context.Context, owner string) (int, error) {
	var kept []*domain.Photo
	n := 0
	for _, p := range r.created {
		if p.OwnerSubject() == owner {
			n++
			continue
		}
		kept = append(kept, p)
	}
	r.created = kept
	return n, nil
}
func (r *stubRepo) ListByOwner(_ context.Context, owner string) ([]*domain.Photo, error) {
	var out []*domain.Photo
	for _, p := range r.created {
		if p.OwnerSubject() == owner {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *stubRepo) ListStalePending(context.Context, time.Time, int) ([]*domain.Photo, error) {
	return nil, nil
}

type stubImages struct {
	deleted []string
	// statErr が非 nil なら Stat が失敗する(実体のない写真の再現)
	statErr error
	// calls は呼び出し順の記録。tx の内か外かは呼び出し順では見えないので、
	// 「Stat の前に Get が終わっている」ことは photo_audit_test で別に見る
	calls []string
}

func (*stubImages) NewKey(string, string) (string, error) { return "photos/stub.png", nil }
func (*stubImages) PresignPut(context.Context, string, string, time.Duration) (usecase.UploadTarget, error) {
	return usecase.UploadTarget{URL: "http://example.test/put", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (*stubImages) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "http://example.test/get", nil
}
func (i *stubImages) Stat(context.Context, string) (usecase.ObjectInfo, error) {
	i.calls = append(i.calls, "stat")
	if i.statErr != nil {
		return usecase.ObjectInfo{}, i.statErr
	}
	return usecase.ObjectInfo{SizeBytes: 1024, ContentType: "image/png"}, nil
}
func (i *stubImages) Delete(_ context.Context, key string) error {
	i.calls = append(i.calls, "delete")
	i.deleted = append(i.deleted, key)
	return nil
}

type stubRelations struct {
	written       []authz.Tuple
	deletedTuples []authz.Tuple
}

func (r *stubRelations) WriteRelations(_ context.Context, t []authz.Tuple) error {
	r.written = append(r.written, t...)
	return nil
}
func (r *stubRelations) DeleteRelations(_ context.Context, t []authz.Tuple) error {
	r.deletedTuples = append(r.deletedTuples, t...)
	return nil
}
