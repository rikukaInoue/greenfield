package flags_test

import (
	"context"
	"testing"

	"github.com/rikukaInoue/greenfield/core/flags"
)

func TestBoolReadsEvaluatedValues(t *testing.T) {
	ctx := flags.WithValues(context.Background(), map[string]bool{"release.x": true})
	if !flags.Bool(ctx, "release.x") {
		t.Error("release.x = false, want true")
	}
	if flags.Bool(ctx, "release.unknown") {
		t.Error("宣言されていないフラグが true になった")
	}
}

// ミドルウェアを通っていない ctx では false（安全側）になる。
func TestBoolWithoutEvaluation(t *testing.T) {
	if flags.Bool(context.Background(), "release.x") {
		t.Error("未評価の ctx で true になった")
	}
}
