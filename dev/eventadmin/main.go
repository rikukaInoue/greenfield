// eventadmin はローカルのイベントバス（LocalStack）にトピック・キュー・購読を用意する。
//
//	go run ./dev/eventadmin ensure-topics
//
// トピック・キューの作成はバケットや database の作成と同じくインフラ側の操作であり、
// サービスのマイグレーションには入れない（本番は IaC が持つ。実機再演は 7.3）。
//
// 構成は internal-03 §2.3: SNS FIFO トピック（送り手ごと）→ SQS FIFO キュー（受け手ごと）の
// ファンアウト。raw message delivery で購読し、受信側は envelope JSON を直接読む。
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// wiring はトピック → 購読キューの配線。受け手サービスが増えたら追記する。
var wiring = []struct {
	topic  string
	queues []string
}{
	{topic: "photo-events.fifo", queues: []string{"gear-photo-events.fifo"}},
}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "ensure-topics" {
		fmt.Fprintln(os.Stderr, "usage: eventadmin ensure-topics")
		os.Exit(2)
	}
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "eventadmin:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(envOr("AWS_REGION", "us-east-1")),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			envOr("AWS_ACCESS_KEY_ID", "test"), envOr("AWS_SECRET_ACCESS_KEY", "testtest"), "")))
	if err != nil {
		return err
	}
	endpoint := envOr("EVENTBUS_ENDPOINT_URL", envOr("AWS_ENDPOINT_URL", "http://localhost:4566"))
	snsc := sns.NewFromConfig(cfg, func(o *sns.Options) { o.BaseEndpoint = aws.String(endpoint) })
	sqsc := sqs.NewFromConfig(cfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String(endpoint) })

	for _, w := range wiring {
		// CreateTopic / CreateQueue は同名なら既存を返す（冪等）
		topic, err := snsc.CreateTopic(ctx, &sns.CreateTopicInput{
			Name: aws.String(w.topic),
			// ContentBasedDeduplication は使わない: MessageDeduplicationId（イベントID）を明示する
			Attributes: map[string]string{"FifoTopic": "true"},
		})
		if err != nil {
			return fmt.Errorf("create topic %s: %w", w.topic, err)
		}
		fmt.Printf("topic %s\n", aws.ToString(topic.TopicArn))

		for _, q := range w.queues {
			queue, err := sqsc.CreateQueue(ctx, &sqs.CreateQueueInput{
				QueueName:  aws.String(q),
				Attributes: map[string]string{"FifoQueue": "true"},
			})
			if err != nil {
				return fmt.Errorf("create queue %s: %w", q, err)
			}
			attrs, err := sqsc.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl:       queue.QueueUrl,
				AttributeNames: []sqstypes.QueueAttributeName{"QueueArn"},
			})
			if err != nil {
				return fmt.Errorf("queue arn %s: %w", q, err)
			}
			qarn := attrs.Attributes["QueueArn"]
			if _, err := snsc.Subscribe(ctx, &sns.SubscribeInput{
				TopicArn: topic.TopicArn,
				Protocol: aws.String("sqs"),
				Endpoint: aws.String(qarn),
				// 受信側が envelope JSON を直接読めるよう SNS の包みを外す
				Attributes: map[string]string{"RawMessageDelivery": "true"},
			}); err != nil {
				return fmt.Errorf("subscribe %s -> %s: %w", w.topic, q, err)
			}
			fmt.Printf("queue %s subscribed to %s\n", aws.ToString(queue.QueueUrl), w.topic)
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
