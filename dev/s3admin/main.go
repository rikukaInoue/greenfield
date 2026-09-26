// s3admin はローカルのオブジェクトストレージにバケットを用意する。
//
//	go run ./dev/s3admin ensure-buckets
//
// バケットの作成は DB の database 作成と同じくインフラ側の操作であり、
// サービスのマイグレーションには入れない。RustFS には init フックがないためここで行う。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// buckets はローカル環境に必要なバケット。新サービスが増えたら追記する。
var buckets = []string{"photo-images"}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "ensure-buckets" {
		fmt.Fprintln(os.Stderr, "usage: s3admin ensure-buckets")
		os.Exit(2)
	}
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "s3admin:", err)
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
	endpoint := envOr("AWS_ENDPOINT_URL", "http://localhost:9000")
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	for _, b := range buckets {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(b)})
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		switch {
		case err == nil:
			fmt.Printf("created bucket %s\n", b)
		case errors.As(err, &owned), errors.As(err, &exists):
			fmt.Printf("bucket %s already exists\n", b)
		default:
			return fmt.Errorf("create bucket %s: %w", b, err)
		}
		if err := putCORS(ctx, client, b); err != nil {
			return err
		}
	}
	return nil
}

// putCORS はフロントエンドのオリジンから署名付きURLへ直接 PUT / GET できるようにする。
func putCORS(ctx context.Context, client *s3.Client, bucket string) error {
	origins := strings.Split(envOr("FRONTEND_ORIGINS", "http://localhost:5173,http://localhost:3000"), ",")
	_, err := client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: aws.String(bucket),
		CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{{
			AllowedOrigins: origins,
			AllowedMethods: []string{"PUT", "GET"},
			AllowedHeaders: []string{"*"},
			MaxAgeSeconds:  aws.Int32(600),
		}}},
	})
	if err != nil {
		return fmt.Errorf("put cors %s: %w", bucket, err)
	}
	fmt.Printf("bucket %s cors: %s\n", bucket, strings.Join(origins, ", "))
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
