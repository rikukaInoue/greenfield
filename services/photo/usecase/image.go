package usecase

import (
	"context"
	"time"
)

// UploadTarget は画像のアップロード先。署名URLは期限付きで、クライアントが直接 PUT する。
type UploadTarget struct {
	URL       string
	ExpiresAt time.Time
}

// ObjectInfo は保存済みオブジェクトの情報。
type ObjectInfo struct {
	SizeBytes   int64
	ContentType string
}

// ImageStore は画像オブジェクトの置き場所。実装は blobstore パッケージが持つ。
type ImageStore interface {
	// NewKey は新しいオブジェクトの鍵を作る。
	NewKey(ownerSubject, contentType string) (string, error)
	// PresignPut は指定の鍵へ PUT するための署名URLを返す。
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (UploadTarget, error)
	// PresignGet は指定の鍵を取得するための署名URLを返す。
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Stat はオブジェクトの情報を返す。存在しなければ ErrObjectNotFound。
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// Delete はオブジェクトを削除する。存在しない鍵の削除も無害。
	Delete(ctx context.Context, key string) error
}
