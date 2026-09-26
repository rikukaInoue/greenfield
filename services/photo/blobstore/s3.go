// Package blobstore は画像オブジェクトの置き場所を S3 互換のストレージで実装する。
// AWS SDK はこのサービスのモジュールに閉じる。
package blobstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/rikukaInoue/greenfield/services/photo/usecase"
)

// Config は接続設定。Endpoint が空なら実際の AWS を使う。
type Config struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
}

// S3Store は S3 互換ストレージ上の ImageStore。
type S3Store struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

// NewS3Store は S3Store を返す。
func NewS3Store(ctx context.Context, cfg Config) (*S3Store, error) {
	opts := []func(*config.LoadOptions) error{config.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blobstore: load config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // LocalStack や MinIO は仮想ホスト形式を使えない
		}
	})
	return &S3Store{bucket: cfg.Bucket, client: client, presign: s3.NewPresignClient(client)}, nil
}

// NewKey は owner/日付/乱数.拡張子 の形で鍵を作る。所有者を鍵に含めるので調査がしやすい。
func (s *S3Store) NewKey(ownerSubject, contentType string) (string, error) {
	ext, ok := extensions[contentType]
	if !ok {
		return "", fmt.Errorf("blobstore: 未知の content_type %q", contentType)
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("blobstore: 乱数: %w", err)
	}
	return fmt.Sprintf("photos/%s/%s/%s%s",
		sanitize(ownerSubject), time.Now().UTC().Format("2006/01/02"), hex.EncodeToString(b[:]), ext), nil
}

var extensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/avif": ".avif",
}

// PresignPut は PUT 用の署名URLを返す。Content-Type は署名に含めるため、
// クライアントは同じ値をヘッダで送る必要がある。
func (s *S3Store) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (usecase.UploadTarget, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return usecase.UploadTarget{}, fmt.Errorf("blobstore: presign put: %w", err)
	}
	return usecase.UploadTarget{URL: req.URL, ExpiresAt: time.Now().Add(ttl)}, nil
}

// PresignGet は GET 用の署名URLを返す。
func (s *S3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("blobstore: presign get: %w", err)
	}
	return req.URL, nil
}

// Stat はオブジェクトの情報を返す。
func (s *S3Store) Stat(ctx context.Context, key string) (usecase.ObjectInfo, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if notFound(err) {
			return usecase.ObjectInfo{}, usecase.ErrObjectNotFound
		}
		return usecase.ObjectInfo{}, fmt.Errorf("blobstore: head object: %w", err)
	}
	info := usecase.ObjectInfo{}
	if out.ContentLength != nil {
		info.SizeBytes = *out.ContentLength
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	return info, nil
}

// Delete はオブジェクトを削除する。
func (s *S3Store) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil && !notFound(err) {
		return fmt.Errorf("blobstore: delete object: %w", err)
	}
	return nil
}

func notFound(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NotFound", "NoSuchKey", "404":
			return true
		}
	}
	return false
}

// sanitize は鍵に使えない文字を落とす。
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
