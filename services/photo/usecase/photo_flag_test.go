package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/flags"
	"github.com/rikukaInoue/greenfield/services/photo/domain"
	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// フラグのテストは flagd を立てず、評価済みの値を ctx へ積んで ON / OFF 両方を通す。
func TestCreateHonorsKillSwitch(t *testing.T) {
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, &stubRepo{}, &stubImages{}, nil, &stubRelations{}, sinkEventual{}, &stubGearLink{}, nil)
	principal := authz.Principal{Subject: "alice", Kind: authz.PrincipalUser}
	in := usecase.CreatePhotoInput{Caption: "x", ContentType: "image/png"}

	t.Run("ON なら投稿できない", func(t *testing.T) {
		ctx := flags.WithValues(authz.WithPrincipal(context.Background(), principal),
			map[string]bool{usecase.FlagDisableUploads: true})
		if _, err := cmds.Create(ctx, in); !errors.Is(err, usecase.ErrUploadsDisabled) {
			t.Fatalf("err = %v, want ErrUploadsDisabled", err)
		}
	})

	t.Run("OFF なら投稿できる", func(t *testing.T) {
		ctx := flags.WithValues(authz.WithPrincipal(context.Background(), principal),
			map[string]bool{usecase.FlagDisableUploads: false})
		res, err := cmds.Create(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if res.Photo.Status() != domain.PendingUpload {
			t.Fatalf("status = %q, want pending_upload", res.Photo.Status())
		}
		if res.Upload.URL == "" {
			t.Error("署名URLが空")
		}
	})

	t.Run("未評価の ctx でも安全側（投稿できる）", func(t *testing.T) {
		ctx := authz.WithPrincipal(context.Background(), principal)
		if _, err := cmds.Create(ctx, in); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})
}

func TestCanCreateMatchesCreate(t *testing.T) {
	cmds := usecase.NewPhotoCommands(consistency.FakeAtomic{}, &stubRepo{}, &stubImages{}, nil, &stubRelations{}, sinkEventual{}, &stubGearLink{}, nil)
	principal := authz.Principal{Subject: "alice", Kind: authz.PrincipalUser}

	cases := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"フラグ OFF", flags.WithValues(authz.WithPrincipal(context.Background(), principal), map[string]bool{usecase.FlagDisableUploads: false}), true},
		{"フラグ ON", flags.WithValues(authz.WithPrincipal(context.Background(), principal), map[string]bool{usecase.FlagDisableUploads: true}), false},
		{"主体なし", context.Background(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmds.CanCreate(tc.ctx); got != tc.want {
				t.Fatalf("CanCreate = %v, want %v", got, tc.want)
			}
			_, err := cmds.Create(tc.ctx, usecase.CreatePhotoInput{Caption: "x", ContentType: "image/png"})
			if (err == nil) != tc.want {
				t.Fatalf("Create err = %v, CanCreate = %v と食い違う", err, tc.want)
			}
		})
	}
}
