// Package consistency は整合性クラス（Atomic / Eventual）の実装を提供する。
package consistency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type txKey struct{}

// Atomic は fn 内のDB操作を1つのトランザクションにまとめる。
type Atomic struct {
	db *sql.DB
}

// NewAtomic は db 上で動く Atomic を返す。
func NewAtomic(db *sql.DB) *Atomic { return &Atomic{db: db} }

// Do は fn を1トランザクションで実行する。fn が error を返せばロールバック、nil ならコミットする。
// すでにトランザクション中の ctx で呼ばれた場合は既存のものに参加する。
//
// fn 内では必ず引数の ctx を使うこと。外側の ctx を使った Repository 呼び出しは
// トランザクションに参加しない。
func (a *Atomic) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := TxFrom(ctx); ok {
		return fn(ctx)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("consistency: begin: %w", err)
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return errors.Join(err, rollback(tx))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("consistency: commit: %w", err)
	}
	return nil
}

// TxFrom は ctx のトランザクションを返す。Repository 実装が sqlc の WithTx へ渡すために使う。
// キーは非公開型のため、UseCase 層から取り出す経路はない。
func TxFrom(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	return tx, ok
}

func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("consistency: rollback: %w", err)
	}
	return nil
}

// FakeAtomic は fn をそのまま実行する Atomic。Repository モックと組み合わせてDBなしで
// UseCase の分岐を検証する用途に使う。トランザクションを張らないためロールバックは検証できない。
type FakeAtomic struct{}

// Do は fn をそのまま実行する。
func (FakeAtomic) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
