package domain_test

import (
	"errors"
	"testing"

	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

func TestNewPhotoRequiresOwner(t *testing.T) {
	if _, err := domain.NewPhoto("", "", domain.Private, nil, "k", "image/jpeg"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestNewPhotoDefaultsToPrivate(t *testing.T) {
	p, err := domain.NewPhoto("alice", "朝の光", "", nil, "k", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if p.Visibility() != domain.Private {
		t.Fatalf("visibility = %q, want private", p.Visibility())
	}
}

func TestPublishIsOneWayTransition(t *testing.T) {
	p, err := domain.NewPhoto("alice", "", domain.Private, nil, "k", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CommitUpload(1024); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(); err != nil {
		t.Fatal(err)
	}
	if p.Visibility() != domain.Public {
		t.Fatalf("visibility = %q, want public", p.Visibility())
	}
	if err := p.Publish(); !errors.Is(err, domain.ErrAlreadyPublished) {
		t.Fatalf("2回目の Publish = %v, want ErrAlreadyPublished", err)
	}
}

func TestParseVisibility(t *testing.T) {
	for _, c := range []struct {
		in   string
		want domain.Visibility
		ok   bool
	}{
		{"", domain.Private, true},
		{"private", domain.Private, true},
		{"public", domain.Public, true},
		{"bogus", "", false},
	} {
		got, err := domain.ParseVisibility(c.in)
		if (err == nil) != c.ok {
			t.Errorf("ParseVisibility(%q) err = %v, ok = %v", c.in, err, c.ok)
			continue
		}
		if got != c.want {
			t.Errorf("ParseVisibility(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGearItemIDMustBePositive(t *testing.T) {
	zero := int64(0)
	if _, err := domain.NewPhoto("alice", "", domain.Private, &zero, "k", "image/jpeg"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestPublishRequiresFinishedUpload(t *testing.T) {
	p, err := domain.NewPhoto("alice", "", domain.Private, nil, "k", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(); !errors.Is(err, domain.ErrUploadNotFinished) {
		t.Fatalf("err = %v, want ErrUploadNotFinished", err)
	}
}

func TestCommitUploadOnlyFromPending(t *testing.T) {
	p, err := domain.NewPhoto("alice", "", domain.Private, nil, "k", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != domain.PendingUpload {
		t.Fatalf("status = %q, want pending_upload", p.Status())
	}
	if err := p.CommitUpload(0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("空の画像 = %v, want ErrInvalid", err)
	}
	if err := p.CommitUpload(1024); err != nil {
		t.Fatal(err)
	}
	if p.Status() != domain.Ready {
		t.Fatalf("status = %q, want ready", p.Status())
	}
	if err := p.CommitUpload(1024); !errors.Is(err, domain.ErrNotPending) {
		t.Fatalf("2回目 = %v, want ErrNotPending", err)
	}
}

func TestRejectsUnknownContentType(t *testing.T) {
	if _, err := domain.NewPhoto("alice", "", domain.Private, nil, "k", "application/pdf"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
