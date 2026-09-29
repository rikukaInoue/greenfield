package usecase

import "context"

// Atomic は原子性の境界。fn 内の操作は全て成功するか、全てなかったことになる。
type Atomic interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
