package usecase

import "context"

// Atomic は原子性の境界。fn 内の操作は全て成功するか、全てなかったことになる。
type Atomic interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// Eventual は結果整合の配送。最終的な到達を保証する（at-least-once）。
// Publish は Atomic.Do の中で呼ぶ。outbox への記録が業務データと同一トランザクションで確定しないと
// 到達保証が成立しない（docs/adr/0012-eventual-publish-inside-atomic.md）。
type Eventual interface {
	Publish(ctx context.Context, event Event) error
}

// Event は他ドメインへ公表する事実。
type Event struct {
	ID          string
	Type        string
	AggregateID string
	Payload     []byte
}
