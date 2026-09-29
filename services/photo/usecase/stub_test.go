package usecase_test

import (
	"context"
	"errors"
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
func (r *stubRepo) ListPendingGearLinks(_ context.Context, _ time.Time, _ int) ([]*domain.Photo, error) {
	var out []*domain.Photo
	for _, p := range r.created {
		if p.GearLinkStatus() == domain.GearLinkPending {
			out = append(out, p)
		}
	}
	return out, nil
}

// stubGearLink は gear の代役。既定は常に受理（linked）。
type stubGearLink struct {
	// down が true なら「gear 停止中」を再現する
	down bool
	// received は受けたキー → 結果（Get の照会に答える）
	received map[string]usecase.GearLinkResult
	// reject が true なら拒否を返す
	reject bool
	links  int
}

func (g *stubGearLink) Link(_ context.Context, _, _ int64, key string) (usecase.GearLinkResult, error) {
	if g.down {
		return usecase.GearLinkResult{}, errors.New("gear down")
	}
	if g.received == nil {
		g.received = map[string]usecase.GearLinkResult{}
	}
	if res, ok := g.received[key]; ok {
		return res, nil // 同じキーは最初の結果（gear 側の冪等性の再現）
	}
	g.links++
	res := usecase.GearLinkResult{Status: "linked"}
	if g.reject {
		res = usecase.GearLinkResult{Status: "rejected", Reason: "item_not_found"}
	}
	g.received[key] = res
	return res, nil
}

func (g *stubGearLink) Get(_ context.Context, key string) (usecase.GearLinkResult, error) {
	if g.down {
		return usecase.GearLinkResult{}, errors.New("gear down")
	}
	res, ok := g.received[key]
	if !ok {
		return usecase.GearLinkResult{}, usecase.ErrLinkNotFound
	}
	return res, nil
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
