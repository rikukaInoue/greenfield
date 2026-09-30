// Package eventbus は SNS FIFO（トピック）/ SQS FIFO（購読キュー）によるイベント配送。
// 送り手は Relay 経由で SNSBus.Publish を呼び、受け手は Consume で SQS をポーリングする
// （internal-03 §2.3。1トピック → 受け手サービスごとのキューへファンアウト）。
//
// ローカル・CI は LocalStack を同じ SDK で使う（AWS_ENDPOINT_URL で切替、実機再演は 7.3）。
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/rikukaInoue/greenfield/core/consistency"
)

// envelope はバスに載せるイベントの形。SNS→SQS の raw message delivery を前提に、
// 受信側はこの JSON を直接読む。
type envelope struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	AggregateID string          `json:"aggregate_id"`
	Payload     json.RawMessage `json:"payload"`
}

// SNSBus は consistency.Bus の SNS FIFO 実装。
type SNSBus struct {
	client   *sns.Client
	topicARN string
}

// NewSNSBus は topicARN へ送る Bus を返す。
func NewSNSBus(client *sns.Client, topicARN string) *SNSBus {
	return &SNSBus{client: client, topicARN: topicARN}
}

// Publish はイベントを SNS FIFO トピックへ送る。
//   - MessageGroupId = 集約ID: 同一集約内の順序保証をバス側でも成立させる
//   - MessageDeduplicationId = イベントID（Republish は run ごとの DedupID）:
//     5分窓の重複をバス側でも弾く（補助。主たる防御は受信側の inbox）。
//     窓は**事故の二重送信**用であり、意図した再生（Republish）は DedupID で迂回する
//     ——迂回しないと5分以内のイベントが黙って落ち、再構築が空振りする（check #10 で実測）
func (b *SNSBus) Publish(ctx context.Context, ev consistency.Event) error {
	body, err := json.Marshal(envelope{ID: ev.ID, Type: ev.Type, AggregateID: ev.AggregateID, Payload: ev.Payload})
	if err != nil {
		return fmt.Errorf("eventbus: encode: %w", err)
	}
	dedup := ev.ID
	if ev.DedupID != "" {
		dedup = ev.DedupID
	}
	_, err = b.client.Publish(ctx, &sns.PublishInput{
		TopicArn:               aws.String(b.topicARN),
		Message:                aws.String(string(body)),
		MessageGroupId:         aws.String(ev.AggregateID),
		MessageDeduplicationId: aws.String(dedup),
	})
	if err != nil {
		return fmt.Errorf("eventbus: publish %s: %w", ev.ID, err)
	}
	return nil
}

// Handler は受信イベントの処理。nil を返すとメッセージを削除する。
// エラーを返すと削除せず、可視性タイムアウト後に再配信される（at-least-once）。
type Handler func(ctx context.Context, ev consistency.Event) error

// Consumer は SQS FIFO キューをポーリングして Handler を呼ぶ。
type Consumer struct {
	client   *sqs.Client
	queueURL string

	// Handler は受信イベントの処理。必須。
	Handler Handler
	// WaitSeconds はロングポーリングの待ち時間。
	WaitSeconds int32
	// DrainTimeout は停止時に受信済みメッセージの処理を完走させる上限。ゼロなら 20 秒(10.8)。
	DrainTimeout time.Duration
}

// NewConsumer は queueURL を購読する Consumer を返す。
func NewConsumer(client *sqs.Client, queueURL string, h Handler) *Consumer {
	return &Consumer{client: client, queueURL: queueURL, Handler: h, WaitSeconds: 5}
}

// Run は ctx のキャンセルまで受信を続ける。
// 処理成功（nil）→ 削除。失敗 → 削除せず再配信に任せる。
// 「削除前に停止すれば再配信される」は前提であり、重複は受信側の inbox が弾く（check #9）。
func (c *Consumer) Run(ctx context.Context) error {
	for {
		if err := c.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				// 受信済み分は RunOnce が完走させている。正常な停止は nil
				// (SIGTERM での終了を異常扱いして exit 1 にしない)
				slog.InfoContext(ctx, "consumer: 処理中を完了させて停止")
				return nil
			}
			slog.ErrorContext(ctx, "consumer: 受信に失敗", "error", err)
			// バス不通等。少し待って続ける
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// RunOnce は1回の ReceiveMessage 分を処理する。
func (c *Consumer) RunOnce(ctx context.Context) error {
	out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(c.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     c.WaitSeconds,
	})
	if err != nil {
		return fmt.Errorf("consumer: receive: %w", err)
	}
	// 受信待ち(ロングポーリング)は ctx で中断してよいが、**受信済みメッセージの処理は
	// 完走させる**(10.8)。途中で切ると「業務処理は済んだのに削除できない」が起こり、
	// 可視性タイムアウト後の再配信=自作の重複を生む(inbox が吸収するとはいえ避けられる)。
	procCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.drainTimeout())
	defer cancel()
	for _, m := range out.Messages {
		var env envelope
		if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &env); err != nil {
			// 読めないメッセージは再配信しても読めない。ログに残して削除する
			// （DLQ は実機再演 7.3 で扱う）
			slog.ErrorContext(procCtx, "consumer: 読めないメッセージ", "error", err, "body", aws.ToString(m.Body))
			c.delete(procCtx, m.ReceiptHandle)
			continue
		}
		ev := consistency.Event{ID: env.ID, Type: env.Type, AggregateID: env.AggregateID, Payload: env.Payload}
		if err := c.Handler(procCtx, ev); err != nil {
			slog.ErrorContext(procCtx, "consumer: 処理に失敗（再配信に任せる）", "event_id", ev.ID, "error", err)
			continue
		}
		c.delete(procCtx, m.ReceiptHandle)
	}
	return nil
}

func (c *Consumer) drainTimeout() time.Duration {
	if c.DrainTimeout > 0 {
		return c.DrainTimeout
	}
	return 20 * time.Second
}

func (c *Consumer) delete(ctx context.Context, receipt *string) {
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl: aws.String(c.queueURL), ReceiptHandle: receipt,
	}); err != nil {
		// 削除に失敗すると再配信される。inbox が弾くので実害は重複受信のログだけ
		slog.WarnContext(ctx, "consumer: 削除に失敗", "error", err)
	}
}
