package usecase

import (
	"errors"
	"os"
	"strings"
	"time"
)

// 検証用の失敗注入点。
const (
	// FaultBeforeRelations はタプル書き込みの直前。業務レコードだけが書かれた状態で落とす。
	FaultBeforeRelations = "before_relations"
	// FaultBeforeCommit はタプル書き込み後・コミット直前。孤児タプルを作る。
	FaultBeforeCommit = "before_commit"
)

// ErrInjectedFault は注入された失敗。
var ErrInjectedFault = errors.New("photo: 注入された失敗")

// FaultInjector は指定の注入点でエラーを返す。
type FaultInjector interface {
	Inject(point string) error
}

// NoFaults は何も注入しない。
type NoFaults struct{}

// Inject は常に nil を返す。
func (NoFaults) Inject(string) error { return nil }

// EnvFaults は環境変数 PHOTO_FAULT に一致する注入点でエラーを返す。ローカル検証用。
type EnvFaults struct{}

// Inject は PHOTO_FAULT が point と一致すればエラーを返す。
// `<point>=<duration>`(例: before_commit=3s)の形なら、失敗でなく**遅延**を注入する。
// graceful shutdown(10.8)のドリルで「処理中のリクエスト」を意図的に作るために使う。
func (EnvFaults) Inject(point string) error {
	v := os.Getenv("PHOTO_FAULT")
	if v == point {
		return ErrInjectedFault
	}
	if rest, ok := strings.CutPrefix(v, point+"="); ok {
		if d, err := time.ParseDuration(rest); err == nil {
			time.Sleep(d)
		}
	}
	return nil
}
