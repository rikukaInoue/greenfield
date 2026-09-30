package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"github.com/rikukaInoue/greenfield/core/consistency"
	"github.com/rikukaInoue/greenfield/core/eventbus"
)

// RelayConfig は relay（outbox → SNS）の設定。
type RelayConfig struct {
	// DSN は outbox を読む接続。アプリ実行用ユーザーで足りる（DML のみ）。
	DSN string
	// TopicARN は photo のイベントを流す SNS FIFO トピック。
	TopicARN string
	// Interval はポーリング周期。
	Interval time.Duration
}

// RelayConfigFromEnv は環境変数から relay の設定を読む。
// 既定はローカルの LocalStack（dev/eventadmin が作るトピック）。
func RelayConfigFromEnv() RelayConfig {
	interval := 2 * time.Second
	if v := os.Getenv("RELAY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	return RelayConfig{
		DSN:      envOr("PHOTO_DSN", "photo_app:photo_app@tcp(127.0.0.1:13306)/photo?parseTime=true"),
		TopicARN: envOr("PHOTO_EVENT_TOPIC_ARN", "arn:aws:sns:us-east-1:000000000000:photo-events.fifo"),
		Interval: interval,
	}
}

func newRelay(ctx context.Context, cfg RelayConfig) (*consistency.Relay, *sql.DB, error) {
	db, err := openDB(cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("photo db: %w", err)
	}
	// AWS_ENDPOINT_URL（LocalStack）・認証情報は SDK の既定解決に任せる（blobstore と同じ流儀）
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("aws config: %w", err)
	}
	bus := eventbus.NewSNSBus(sns.NewFromConfig(awsCfg), cfg.TopicARN)
	return consistency.NewRelay(db, bus), db, nil
}

// RunRelay は relay を常駐で回す。停止しても未送信レコードが outbox に残るだけで、
// 再開すれば回復する（check #8）。
func RunRelay(ctx context.Context, cfg RelayConfig) error {
	relay, db, err := newRelay(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return relay.Run(ctx, cfg.Interval)
}

// Republish は since 以降のイベントを outbox から再送する（再生の正は outbox。check #10）。
// 受信側の inbox が重複を吸収する前提の管理コマンド。
func Republish(ctx context.Context, cfg RelayConfig, since time.Time) (int, error) {
	relay, db, err := newRelay(ctx, cfg)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return relay.Republish(ctx, since)
}
