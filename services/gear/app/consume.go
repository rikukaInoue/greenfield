package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/eventbus"
	"github.com/rikukaInoue/greenfield/services/gear/replicaview"
	"github.com/rikukaInoue/greenfield/services/gear/usecase"
)

// ConsumeConfig はイベント消費（SQS → inbox → 業務処理）の設定。
type ConsumeConfig struct {
	// DSN は gear の業務データ（inbox を含む）。
	DSN string
	// QueueURL は photo のイベントを受ける SQS FIFO キュー。
	QueueURL string
}

// ConsumeConfigFromEnv は環境変数から消費側の設定を読む。
// 既定はローカルの LocalStack（dev/eventadmin が作るキュー）。
func ConsumeConfigFromEnv() ConsumeConfig {
	return ConsumeConfig{
		DSN:      envOr("GEAR_DSN", "gear_app:gear_app@tcp(127.0.0.1:13306)/gear?parseTime=true"),
		QueueURL: envOr("GEAR_EVENT_QUEUE_URL", "http://localhost:4566/000000000000/gear-photo-events.fifo"),
	}
}

// inboxAdapter は usecase.Inbox を core/consistency の Inbox で満たす。
// エラーの語彙も usecase 側へ変換する（usecase は core/consistency を知らない）。
type inboxAdapter struct{ i consistency.Inbox }

func (a inboxAdapter) MarkProcessed(ctx context.Context, ev usecase.Event) error {
	err := a.i.MarkProcessed(ctx, consistency.Event{
		ID: ev.ID, Type: ev.Type, AggregateID: ev.AggregateID, Payload: ev.Payload,
	})
	if errors.Is(err, consistency.ErrDuplicateEvent) {
		return usecase.ErrDuplicateEvent
	}
	return err
}

// RunConsumer は SQS のポーリングを常駐で回す。
// 処理は「inbox 記録 + 業務処理」を同一 tx で行い、成功したものだけ削除する。
// 削除前の停止は再配信になるが inbox が弾く（check #9）。
func RunConsumer(ctx context.Context, cfg ConsumeConfig) error {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("gear db: %w", err)
	}
	defer db.Close()
	events := usecase.NewPhotoEvents(consistency.NewAtomic(db), inboxAdapter{}, replicaview.NewPhotoReplica(db))

	// AWS_ENDPOINT_URL（LocalStack）・認証情報は SDK の既定解決に任せる
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	consumer := eventbus.NewConsumer(sqs.NewFromConfig(awsCfg), cfg.QueueURL,
		func(ctx context.Context, ev consistency.Event) error {
			applied, err := events.Process(ctx, usecase.Event{
				ID: ev.ID, Type: ev.Type, AggregateID: ev.AggregateID, Payload: ev.Payload,
			})
			if err != nil {
				return err
			}
			if applied {
				slog.InfoContext(ctx, "consumer: 適用", "event_id", ev.ID, "type", ev.Type)
			}
			return nil
		})
	slog.Info("consumer: 受信開始", "queue", cfg.QueueURL, "endpoint", os.Getenv("AWS_ENDPOINT_URL"))
	return consumer.Run(ctx)
}

// RebuildReplica は photo_replica と対応する inbox 記録を消し、再構築の起点を作る。
// このあと photo 側で `photo republish --since` を実行すると consumer が再適用する
// （再生の正は outbox。check #10）。
func RebuildReplica(ctx context.Context, cfg ConsumeConfig) error {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("gear db: %w", err)
	}
	defer db.Close()
	r := replicaview.NewPhotoReplica(db)
	if err := r.Rebuild(ctx, []string{"photo.published", "photo.deleted"}); err != nil {
		return err
	}
	fp, n, err := r.Fingerprint(ctx)
	if err != nil {
		return err
	}
	slog.Info("replica を初期化", "rows", n, "fingerprint", fp)
	return nil
}

// ReplicaStatus は複製の行数と指紋を出す（check #10 の一致検査に使う）。
func ReplicaStatus(ctx context.Context, cfg ConsumeConfig) error {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("gear db: %w", err)
	}
	defer db.Close()
	fp, n, err := replicaview.NewPhotoReplica(db).Fingerprint(ctx)
	if err != nil {
		return err
	}
	// 機械で読む出力なので1行の固定形式にする
	fmt.Printf("rows=%d fingerprint=%s\n", n, fp)
	return nil
}
