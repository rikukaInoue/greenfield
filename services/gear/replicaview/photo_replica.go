// Package replicaview は他ドメインの公開データの読み取り専用の複製（ReplicaView）。
// 正は相手にあり、イベント購読で追従する。表示専用であり業務判断に使わない。
// domain / usecase からこのパッケージを import することは depguard で禁止している（.golangci.yml）。
package replicaview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/services/gear/replicaview/internal/sqlcgen"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// PhotoReplica は photo の公開データの複製の書き込み口。usecase.PhotoReplica の実装。
// 書き込みは受信処理（inbox 記録と同一 tx）からのみ行うため、repository と同じ
// TxFrom / WithTx で ctx のトランザクションに参加する。
type PhotoReplica struct {
	q  *sqlcgen.Queries
	db *sql.DB
}

// NewPhotoReplica は PhotoReplica を返す。
func NewPhotoReplica(db *sql.DB) *PhotoReplica {
	return &PhotoReplica{q: sqlcgen.New(db), db: db}
}

func (r *PhotoReplica) queries(ctx context.Context) *sqlcgen.Queries {
	if tx, ok := consistency.TxFrom(ctx); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}

// Upsert は複製を作成・更新する。同一イベントの再適用は無害（自然冪等）。
func (r *PhotoReplica) Upsert(ctx context.Context, row usecase.PhotoReplicaRow) error {
	created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return fmt.Errorf("replicaview: created_at が読めない %q: %w", row.CreatedAt, err)
	}
	if err := r.queries(ctx).UpsertPhotoReplica(ctx, sqlcgen.UpsertPhotoReplicaParams{
		PhotoID: uint64(row.PhotoID), GearItemID: uint64(row.GearItemID),
		Caption: row.Caption, PhotoCreatedAt: created,
	}); err != nil {
		return fmt.Errorf("replicaview: upsert: %w", err)
	}
	return nil
}

// Delete は複製を落とす。存在しない行の削除も無害。
func (r *PhotoReplica) Delete(ctx context.Context, photoID int64) error {
	if err := r.queries(ctx).DeletePhotoReplica(ctx, uint64(photoID)); err != nil {
		return fmt.Errorf("replicaview: delete: %w", err)
	}
	return nil
}

// Rebuild は複製と処理済み記録を消して再構築の起点を作る（check #10 の管理操作）。
// inbox も消すのが要点: 複製を消しただけでは、outbox からの再生イベントを
// inbox が「処理済み」として弾き、**再構築が静かに空振りする**。
// このあと送信側で `photo republish --since` を実行すると consumer が再適用する。
func (r *PhotoReplica) Rebuild(ctx context.Context, eventTypes []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replicaview: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit 後の Rollback は no-op
	if err := r.q.WithTx(tx).TruncatePhotoReplica(ctx); err != nil {
		return fmt.Errorf("replicaview: truncate: %w", err)
	}
	for _, t := range eventTypes {
		if _, err := tx.ExecContext(ctx, `DELETE FROM inbox WHERE event_type = ?`, t); err != nil {
			return fmt.Errorf("replicaview: inbox の掃除: %w", err)
		}
	}
	return tx.Commit()
}

// Fingerprint は複製の内容の指紋を返す（check #10 の一致検査に使う）。
func (r *PhotoReplica) Fingerprint(ctx context.Context) (string, int64, error) {
	fp, err := r.q.ChecksumPhotoReplica(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("replicaview: checksum: %w", err)
	}
	n, err := r.q.CountPhotoReplica(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("replicaview: count: %w", err)
	}
	// mysql ドライバは文字列集計を []byte で返す
	s := ""
	switch v := fp.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return "", 0, fmt.Errorf("replicaview: checksum の型が想定外: %T", fp)
	}
	return s, n, nil
}
