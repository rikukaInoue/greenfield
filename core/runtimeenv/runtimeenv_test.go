package runtimeenv_test

import (
	"testing"

	"github.com/rikukaInoue/greenfield/core/runtimeenv"
)

// 許可リストなので、未設定や綴り違いは本番として扱う（fail-closed）。
func TestCurrent(t *testing.T) {
	for env, want := range map[string]runtimeenv.Kind{
		"dev":         runtimeenv.Dev,
		"development": runtimeenv.Dev,
		"local":       runtimeenv.Dev,
		"test":        runtimeenv.Test,
		"ci":          runtimeenv.CI,
		"production":  runtimeenv.Prod,
		"prod":        runtimeenv.Prod,
		"prd":         runtimeenv.Prod, // 綴り違いを開発と誤認しない
		"staging":     runtimeenv.Prod,
		"":            runtimeenv.Prod, // 未設定も本番として扱う
	} {
		t.Run("ENV="+env, func(t *testing.T) {
			t.Setenv("ENV", env)
			if got := runtimeenv.Current(); got != want {
				t.Errorf("Current() = %q, want %q", got, want)
			}
		})
	}
}

func TestRequireDevelopment(t *testing.T) {
	for env, allowed := range map[string]bool{
		"dev": true, "test": true, "ci": true,
		"production": false, "prod": false, "prd": false, "staging": false, "": false,
	} {
		t.Run("ENV="+env, func(t *testing.T) {
			t.Setenv("ENV", env)
			err := runtimeenv.RequireDevelopment("テスト用の実装")
			if allowed && err != nil {
				t.Errorf("許されるべきなのに拒否された: %v", err)
			}
			if !allowed && err == nil {
				t.Error("拒否されるべきなのに通った")
			}
		})
	}
}
