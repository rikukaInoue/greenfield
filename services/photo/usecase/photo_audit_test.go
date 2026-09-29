package usecase_test

// docs/audit-2026-09-27.md の A-3 / C-1 / C-3 の回帰テスト(#85)。
// いずれも「表に出ないはず」と書かれていた状態が実際は表に出ていた、という種類の穴。

import (
	"context"
	"errors"
	"testing"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// stubAuthorizer は action ごとに許可を決める。operator 経路と owner 経路の
// 違いを再現するために要る(localauthz の fromParent は viewer/editor だけ)。
type stubAuthorizer struct {
	allow map[string]bool
	asked []authz.Request
}

func (a *stubAuthorizer) Can(_ context.Context, req authz.Request) (authz.Result, error) {
	a.asked = append(a.asked, req)
	return authz.Result{Allowed: a.allow[req.Action]}, nil
}

func newOwnedPhoto(t *testing.T, repo *stubRepo, owner string) *domain.Photo {
	t.Helper()
	caption, err := domain.ParseCaption("x")
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewPhoto(owner, caption, domain.Private, nil, "photos/"+owner+".png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// txTracker は「今 tx の中か」を持つ Atomic。
//
// C-1 の検証には内か外かの観測が必要で、FakeAtomic は単に fn を呼ぶだけなので
// **古い実装(tx 内で Stat)でもテストが通ってしまう**。ここを間違えると、
// 直したことを確かめたつもりで何も確かめていないテストになる。
type txTracker struct {
	inside bool
	depth  int
}

func (a *txTracker) Do(ctx context.Context, fn func(context.Context) error) error {
	a.inside = true
	a.depth++
	defer func() { a.inside = false }()
	return fn(ctx)
}

// txAwareImages は tx の内側から呼ばれたら失敗するストレージ。
type txAwareImages struct {
	*stubImages
	tx *txTracker
}

func (i *txAwareImages) Stat(ctx context.Context, key string) (usecase.ObjectInfo, error) {
	if i.tx.inside {
		return usecase.ObjectInfo{}, errors.New("Stat が tx の中で呼ばれた(行ロックを握って外部を待っている)")
	}
	return i.stubImages.Stat(ctx, key)
}

func (i *txAwareImages) Delete(ctx context.Context, key string) error {
	if i.tx.inside {
		return errors.New("Delete が tx の中で呼ばれた")
	}
	return i.stubImages.Delete(ctx, key)
}

// C-1: CommitUpload は Atomic の中で S3 を呼んではいけない(内部-03 の Atomic
// 条件(2)を S3 は満たさない)。tx の内側で呼ばれたら失敗するストレージで観測する。
func TestCommitUploadKeepsStorageOutOfTransaction(t *testing.T) {
	repo := &stubRepo{}
	tx := &txTracker{}
	images := &txAwareImages{stubImages: &stubImages{}, tx: tx}
	authorizer := &stubAuthorizer{allow: map[string]bool{usecase.ActionEdit: true}}
	cmds := usecase.NewPhotoCommands(tx, repo, images, authorizer, &stubRelations{}, sinkEventual{}, nil)

	p := newOwnedPhoto(t, repo, "alice")
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})

	got, err := cmds.CommitUpload(ctx, p.ID())
	if err != nil {
		t.Fatalf("CommitUpload: %v", err)
	}
	if got.Status() != domain.Ready {
		t.Errorf("status = %v, want ready", got.Status())
	}
	if len(images.stubImages.calls) != 1 || images.stubImages.calls[0] != "stat" {
		t.Errorf("storage calls = %v, want [stat]", images.stubImages.calls)
	}
}

// C-1(続き): 実体が無ければ遷移しない。DB にあるが画像がない状態を表示経路に出さない。
func TestCommitUploadWithoutObjectStaysPending(t *testing.T) {
	repo := &stubRepo{}
	tx := &txTracker{}
	images := &txAwareImages{stubImages: &stubImages{statErr: usecase.ErrObjectNotFound}, tx: tx}
	authorizer := &stubAuthorizer{allow: map[string]bool{usecase.ActionEdit: true}}
	cmds := usecase.NewPhotoCommands(tx, repo, images, authorizer, &stubRelations{}, sinkEventual{}, nil)

	p := newOwnedPhoto(t, repo, "alice")
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})

	if _, err := cmds.CommitUpload(ctx, p.ID()); !errors.Is(err, usecase.ErrObjectNotFound) {
		t.Fatalf("err = %v, want ErrObjectNotFound", err)
	}
	if p.Status() != domain.PendingUpload {
		t.Errorf("status = %v, want pending_upload", p.Status())
	}
}

// C-3: アカウント削除はオブジェクトとタプルまで消す。
// 行だけ消すと object_key を二度と辿れず、画像がバケットに残り続ける。
func TestDeleteByOwnerRemovesObjectsAndTuples(t *testing.T) {
	repo := &stubRepo{}
	tx := &txTracker{}
	images := &txAwareImages{stubImages: &stubImages{}, tx: tx}
	relations := &stubRelations{}
	authorizer := &stubAuthorizer{allow: map[string]bool{usecase.ActionOperate: true}}
	cmds := usecase.NewPhotoCommands(tx, repo, images, authorizer, relations, sinkEventual{}, nil)

	alice := newOwnedPhoto(t, repo, "alice")
	_ = newOwnedPhoto(t, repo, "bob") // 他人の写真は残る

	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "op", Kind: authz.PrincipalUser})
	n, err := cmds.DeleteByOwner(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if len(images.stubImages.deleted) != 1 || images.stubImages.deleted[0] != alice.ObjectKey() {
		t.Errorf("削除したオブジェクト = %v, want [%s]", images.stubImages.deleted, alice.ObjectKey())
	}
	if len(relations.deletedTuples) != 1 {
		t.Fatalf("消したタプル = %v, want 1 本", relations.deletedTuples)
	}
	if got := relations.deletedTuples[0]; got.Relation != "owner" || got.Subject != authz.UserRef("alice") {
		t.Errorf("タプル = %+v", got)
	}
	// 他人の行は残っている
	if left, _ := repo.ListByOwner(ctx, "bob"); len(left) != 1 {
		t.Errorf("他人の写真を消している")
	}
	// 無害な側から: オブジェクト → 行 → タプル。逆順だと「行が無いのに画像が残る」
	if len(images.stubImages.calls) == 0 || images.stubImages.calls[0] != "delete" {
		t.Errorf("オブジェクトを先に消していない: %v", images.stubImages.calls)
	}
}

// C-3: 認可を通らない主体は何も消せない。以前は Can を一切呼んでいなかった。
func TestDeleteByOwnerRequiresOperator(t *testing.T) {
	repo := &stubRepo{}
	images := &stubImages{}
	relations := &stubRelations{}
	// owner 相当の権限だけある主体(operator ではない)
	authorizer := &stubAuthorizer{allow: map[string]bool{usecase.ActionEdit: true}}
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, repo, images, authorizer, relations, sinkEventual{}, nil)

	newOwnedPhoto(t, repo, "alice")
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})

	if _, err := cmds.DeleteByOwner(ctx, "alice"); !errors.Is(err, usecase.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(images.deleted) != 0 || len(relations.deletedTuples) != 0 {
		t.Errorf("拒否したのに消している: images=%v tuples=%v", images.deleted, relations.deletedTuples)
	}
	// 判定は operator で聞く。owner relation にマップされる action で聞くと、
	// operator は viewer/editor しか継承しないのでこの経路は必ず拒否される
	if len(authorizer.asked) != 1 || authorizer.asked[0].Action != usecase.ActionOperate {
		t.Errorf("聞いた相手が違う: %+v", authorizer.asked)
	}
}
