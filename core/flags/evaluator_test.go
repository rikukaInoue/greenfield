package flags_test

import (
	"context"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"

	"github.com/rikukaInoue/greenfield/core/authz"
	"github.com/rikukaInoue/greenfield/core/flags"
)

// Evaluator がプロバイダを実際に引き、宣言した全フラグを ctx へ積むことを確かめる。
// プロバイダ登録を通る経路をテストで押さえておく（登録が無名だと複数サービスで壊れる）。
func TestEvaluatorReadsProvider(t *testing.T) {
	const domain = "test-evaluator"
	registerMemProvider(t, domain, map[string]memprovider.InMemoryFlag{
		"release.on":  boolFlag(true),
		"release.off": boolFlag(false),
	})

	set := flags.Set{
		{Name: "release.on", Default: false},
		{Name: "release.off", Default: true},
		{Name: "release.missing", Default: true}, // プロバイダに無い → 既定値
	}
	got := flags.NewEvaluator(domain, set).Evaluate(context.Background())

	for name, want := range map[string]bool{
		"release.on":      true,
		"release.off":     false,
		"release.missing": true,
	} {
		if got[name] != want {
			t.Errorf("%s = %v, want %v", name, got[name], want)
		}
	}
}

// ドメインが違えば別のプロバイダを引く（1プロセスに複数サービスが載っても混ざらない）。
func TestNamedProvidersAreIsolated(t *testing.T) {
	registerMemProvider(t, "svc-a", map[string]memprovider.InMemoryFlag{"release.x": boolFlag(true)})
	registerMemProvider(t, "svc-b", map[string]memprovider.InMemoryFlag{"release.x": boolFlag(false)})

	set := flags.Set{{Name: "release.x", Default: false}}
	if v := flags.NewEvaluator("svc-a", set).Evaluate(context.Background())["release.x"]; !v {
		t.Error("svc-a の release.x = false, want true")
	}
	if v := flags.NewEvaluator("svc-b", set).Evaluate(context.Background())["release.x"]; v {
		t.Error("svc-b の release.x = true, want false（ドメインが混ざっている）")
	}
}

// ミドルウェアが評価結果を ctx へ積み、ハンドラが flags.Bool で読めることを確かめる。
func TestMiddlewarePutsValuesInContext(t *testing.T) {
	const domain = "test-middleware"
	registerMemProvider(t, domain, map[string]memprovider.InMemoryFlag{"release.on": boolFlag(true)})

	e := flags.NewEvaluator(domain, flags.Set{{Name: "release.on", Default: false}})
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{Subject: "alice", Kind: authz.PrincipalUser})
	if !flags.Bool(flags.WithValues(ctx, e.Evaluate(ctx)), "release.on") {
		t.Error("評価結果が ctx から読めない")
	}
}

func registerMemProvider(t *testing.T, domain string, defs map[string]memprovider.InMemoryFlag) {
	t.Helper()
	if err := openfeature.SetNamedProviderAndWait(domain, memprovider.NewInMemoryProvider(defs)); err != nil {
		t.Fatalf("プロバイダの登録に失敗: %v", err)
	}
}

func boolFlag(v bool) memprovider.InMemoryFlag {
	variant := "off"
	if v {
		variant = "on"
	}
	return memprovider.InMemoryFlag{
		State:          memprovider.Enabled,
		DefaultVariant: variant,
		Variants:       map[string]any{"on": true, "off": false},
	}
}
