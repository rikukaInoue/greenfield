// Package runtimeenv は実行環境の種別を判定する。
// 開発用の実装（署名検証のないトークン、固定の資格情報）を本番へ持ち込まないための入口である。
package runtimeenv

import (
	"fmt"
	"os"
	"slices"
)

// Kind は実行環境の種別。
type Kind string

const (
	Dev  Kind = "dev"
	Test Kind = "test"
	CI   Kind = "ci"
	Prod Kind = "prod"
)

// 開発用の実装を許す環境。許可リストにすることで、ENV の未設定や綴り違いを
// 「本番ではない」と誤認しない（拒否リストだと ENV=prd や未設定が通り抜ける）。
var developmentKinds = []Kind{Dev, Test, CI}

// Current は ENV から種別を返す。既定は Prod（fail-closed）。
func Current() Kind {
	switch os.Getenv("ENV") {
	case "dev", "development", "local", "":
		// 未設定は開発とみなさない。下の default へ落とさず明示する
		if os.Getenv("ENV") == "" {
			return Prod
		}
		return Dev
	case "test":
		return Test
	case "ci":
		return CI
	default:
		return Prod
	}
}

// RequireDevelopment は開発用の実装が許される環境かを確かめる。
// 許されない場合は理由付きのエラーを返す。what には拒否された実装の名前を渡す。
func RequireDevelopment(what string) error {
	k := Current()
	if slices.Contains(developmentKinds, k) {
		return nil
	}
	return fmt.Errorf("%s は開発用の実装であり ENV=%s では使用できない（ENV を %v のいずれかにするか、本番用の実装を配線する）",
		what, envOrUnset(), developmentKinds)
}

func envOrUnset() string {
	if v := os.Getenv("ENV"); v != "" {
		return v
	}
	return "(未設定)"
}
