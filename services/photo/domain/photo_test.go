package domain_test

import (
	"errors"
	"testing"

	"github.com/rikukaInoue/greenfield/services/photo/domain"
)

func TestNewPhotoRequiresOwner(t *testing.T) {
	if _, err := domain.NewPhoto("", "", domain.Private, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestNewPhotoDefaultsToPrivate(t *testing.T) {
	p, err := domain.NewPhoto("alice", "朝の光", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Visibility() != domain.Private {
		t.Fatalf("visibility = %q, want private", p.Visibility())
	}
}

func TestPublishIsOneWayTransition(t *testing.T) {
	p, err := domain.NewPhoto("alice", "", domain.Private, nil)
	if err != nil {
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
	if _, err := domain.NewPhoto("alice", "", domain.Private, &zero); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
