package usecase_test

import (
	"context"
	"testing"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

func linkCmds(gear *stubGearLink) (*usecase.PhotoCommands, *stubRepo) {
	repo := &stubRepo{}
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, repo, &stubImages{},
		&stubAuthorizer{allow: map[string]bool{usecase.ActionEdit: true}}, &stubRelations{},
		sinkEventual{}, gear, nil)
	return cmds, repo
}

func authedCtx() context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})
}

func createWithGear(t *testing.T, cmds *usecase.PhotoCommands) *domain.Photo {
	t.Helper()
	item := int64(1)
	res, err := cmds.Create(authedCtx(), usecase.CreatePhotoInput{
		Caption: "c", ContentType: "image/png", GearItemID: &item,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.Photo
}

func TestCreateSettlesLinkWhenGearIsUp(t *testing.T) {
	gear := &stubGearLink{}
	cmds, _ := linkCmds(gear)
	p := createWithGear(t, cmds)
	if p.GearLinkStatus() != domain.GearLinked {
		t.Fatalf("gear 稼働中は同期で確定するはず: %s", p.GearLinkStatus())
	}
	if p.GearLinkKey() == "" {
		t.Fatal("冪等キーが採番されていない")
	}
}

func TestCreateStaysPendingWhenGearIsDown(t *testing.T) {
	// gear 停止中でも投稿は成立し、紐付けは pending に留まる（失敗にしない）
	gear := &stubGearLink{down: true}
	cmds, _ := linkCmds(gear)
	p := createWithGear(t, cmds)
	if p.GearLinkStatus() != domain.GearLinkPending {
		t.Fatalf("gear 停止中は pending のはず: %s", p.GearLinkStatus())
	}
}

func TestReclaimSettlesPendingWithSameKey(t *testing.T) {
	gear := &stubGearLink{down: true}
	cmds, _ := linkCmds(gear)
	p := createWithGear(t, cmds)
	keyAtCreate := p.GearLinkKey()

	gear.down = false // gear 復旧
	n, err := cmds.ReclaimGearLinks(context.Background(), 0, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if p.GearLinkStatus() != domain.GearLinked {
		t.Fatalf("回収で確定するはず: %s", p.GearLinkStatus())
	}
	// 再送は**同じキー**で行われ、gear への紐付けは1回だけ（check #11 の「二重紐付けなし」）
	if p.GearLinkKey() != keyAtCreate {
		t.Fatal("回収でキーが変わっている（重複排除が壊れる）")
	}
	if gear.links != 1 {
		t.Fatalf("紐付けが %d 回起きている", gear.links)
	}
}

func TestReclaimUsesStoredResultWhenGearAlreadyReceived(t *testing.T) {
	// gear は受理済みだが結果の反映前に photo が落ちた、という中断を再現:
	// 回収は再送ではなく**照会**で確定し、副作用は増えない
	gear := &stubGearLink{}
	cmds, repo := linkCmds(gear)
	p := createWithGear(t, cmds)
	// 反映前の中断を作る: 確定済みの状態を pending に巻き戻す（キーは同じ）
	repo.created[0] = domain.Restore(domain.Restored{
		ID: p.ID(), OwnerSubject: "alice", GearItemID: p.GearItemID(),
		ObjectKey: "k", ContentType: "image/png", Status: domain.PendingUpload,
		GearLinkStatus: domain.GearLinkPending, GearLinkKey: p.GearLinkKey(),
	})
	n, err := cmds.ReclaimGearLinks(context.Background(), 0, 10)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if gear.links != 1 {
		t.Fatalf("照会で済むはずが再紐付けが起きた: %d", gear.links)
	}
	if repo.created[0].GearLinkStatus() != domain.GearLinked {
		t.Fatalf("確定していない: %s", repo.created[0].GearLinkStatus())
	}
}

func TestRejectedIsSettledAsRejected(t *testing.T) {
	gear := &stubGearLink{reject: true}
	cmds, _ := linkCmds(gear)
	p := createWithGear(t, cmds)
	if p.GearLinkStatus() != domain.GearLinkRejected {
		t.Fatalf("拒否が反映されない: %s", p.GearLinkStatus())
	}
	if p.GearItemID() == nil {
		t.Fatal("拒否で gear_item_id が消えている（何に拒否されたかが消える）")
	}
}

func TestCreateWithoutGearItemHasNoLink(t *testing.T) {
	gear := &stubGearLink{}
	cmds, _ := linkCmds(gear)
	res, err := cmds.Create(authedCtx(), usecase.CreatePhotoInput{Caption: "c", ContentType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Photo.GearLinkStatus() != domain.GearLinkNone || gear.links != 0 {
		t.Fatalf("機材未指定で紐付けが動いている: %s links=%d", res.Photo.GearLinkStatus(), gear.links)
	}
}

func TestReclaimSkipsWhileGearStillDown(t *testing.T) {
	gear := &stubGearLink{down: true}
	cmds, _ := linkCmds(gear)
	p := createWithGear(t, cmds)
	n, err := cmds.ReclaimGearLinks(context.Background(), 0, 10)
	if err != nil || n != 0 {
		t.Fatalf("停止中は確定できないはず: n=%d err=%v", n, err)
	}
	if p.GearLinkStatus() != domain.GearLinkPending {
		t.Fatalf("pending のままのはず: %s", p.GearLinkStatus())
	}
}
