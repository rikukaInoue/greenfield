package consistency

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Bus はイベントの配送先（SNS 等）。実装は core/eventbus。
type Bus interface {
	Publish(ctx context.Context, event Event) error
}

// Relay は outbox の未送信イベントをポーリングして Bus へ送る常駐処理（internal-03 §2.3）。
// 停止・失敗しても未送信レコードが DB に残るだけなので、再開すれば回復する（check #8）。
type Relay struct {
	db  *sql.DB
	bus Bus

	// Batch は1回のポーリングで読む最大件数。
	Batch int
	// MaxAttempts を超えたイベントは削除せずエラー状態として残す（監視対象。
	// 失われた通知は障害対応の起点になるため黙って捨てない）。
	MaxAttempts int
}

// NewRelay は db の outbox を bus へ送る Relay を返す。
func NewRelay(db *sql.DB, bus Bus) *Relay {
	return &Relay{db: db, bus: bus, Batch: 100, MaxAttempts: 8}
}

// Run は interval ごとに RunOnce を繰り返す。ctx のキャンセルで抜ける。
func (r *Relay) Run(ctx context.Context, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := r.RunOnce(ctx); err != nil {
			// 一時的な失敗（バス不通等）は次の周期で再試行する。ここで死ぬと
			// 「relay の停止」が増えるだけで何も良くならない
			slog.ErrorContext(ctx, "relay: 送信に失敗", "error", err)
		} else if n > 0 {
			slog.InfoContext(ctx, "relay: 送信", "count", n)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// RunOnce は未送信イベントを id 順に送る。送れた件数を返す。
//
// 順序は同一集約内でのみ保証する（internal-03 §2.3）。送信に失敗した集約は
// その回の以降のイベントをスキップする——先行イベントを飛ばして後続を送ると
// 集約内の順序が壊れるため。他の集約は影響を受けない。
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, event_id, event_type, aggregate_id, payload, attempts,
		       (next_attempt_at IS NULL OR next_attempt_at <= NOW(6)) AS due
		FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT ?`, r.Batch)
	if err != nil {
		return 0, fmt.Errorf("relay: outbox の読み取り: %w", err)
	}
	type row struct {
		rowID    int64
		ev       Event
		attempts int
		due      bool
	}
	var pending []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.rowID, &x.ev.ID, &x.ev.Type, &x.ev.AggregateID, &x.ev.Payload, &x.attempts, &x.due); err != nil {
			rows.Close()
			return 0, fmt.Errorf("relay: scan: %w", err)
		}
		pending = append(pending, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("relay: rows: %w", err)
	}

	sent := 0
	blocked := map[string]bool{} // 集約ID → この回はもう送らない
	for _, x := range pending {
		if blocked[x.ev.AggregateID] {
			continue
		}
		if x.attempts >= r.MaxAttempts {
			// エラー状態。残して監視で拾う。後続も順序のため止める
			blocked[x.ev.AggregateID] = true
			continue
		}
		if !x.due {
			// バックオフ待ち。後続も順序のため止める
			blocked[x.ev.AggregateID] = true
			continue
		}
		if err := r.bus.Publish(ctx, x.ev); err != nil {
			blocked[x.ev.AggregateID] = true
			backoff := time.Duration(1<<uint(min(x.attempts, 6))) * time.Second
			if _, uerr := r.db.ExecContext(ctx, `
				UPDATE outbox SET attempts = attempts + 1, last_error = ?, next_attempt_at = ?
				WHERE id = ?`, err.Error(), time.Now().Add(backoff), x.rowID); uerr != nil {
				return sent, fmt.Errorf("relay: 失敗の記録: %w", uerr)
			}
			continue
		}
		// 「送信成功」と「送信済み記録」の間で停止しうる → 同一イベントの再送になる。
		// at-least-once の前提どおり、受信側の inbox が吸収する（check #9）
		if _, err := r.db.ExecContext(ctx,
			`UPDATE outbox SET published_at = NOW(6), last_error = NULL WHERE id = ?`, x.rowID); err != nil {
			return sent, fmt.Errorf("relay: 送信済みの記録: %w", err)
		}
		sent++
	}
	return sent, nil
}

// Republish は期間内のイベントを送信済みかどうかに関わらず bus へ再送する。
// SQS は再生できないため**再生の正は outbox**（internal-03 §2.3、check #10）。
// 再送も at-least-once の一形態であり、受信側の冪等性（inbox）で吸収される。
//
// バス側の重複排除には ID ではなく run ごとの DedupID を使う。ID のままだと
// 5分以内に送られたイベントが SNS FIFO の窓で**黙って落ち**、再構築が空振りする。
func (r *Relay) Republish(ctx context.Context, since time.Time) (int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT event_id, event_type, aggregate_id, payload
		FROM outbox WHERE created_at >= ? ORDER BY id`, since)
	if err != nil {
		return 0, fmt.Errorf("relay: outbox の読み取り: %w", err)
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.AggregateID, &ev.Payload); err != nil {
			return 0, fmt.Errorf("relay: scan: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("relay: rows: %w", err)
	}
	run := time.Now().UnixNano()
	sent := 0
	for _, ev := range events {
		ev.DedupID = fmt.Sprintf("repub-%d-%s", run, ev.ID)
		if err := r.bus.Publish(ctx, ev); err != nil {
			return sent, fmt.Errorf("relay: 再送 %s: %w", ev.ID, err)
		}
		sent++
	}
	return sent, nil
}
