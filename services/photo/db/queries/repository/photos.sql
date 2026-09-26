-- コマンド側（Entity の復元・保存）のクエリ。参照できるのは自ドメイン（photo）のテーブルのみ。

-- name: CreatePhoto :execresult
INSERT INTO photos (owner_subject, title, visibility, gear_item_id, object_key, content_type, status)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetPhotoForUpdate :one
SELECT * FROM photos WHERE id = ? FOR UPDATE;

-- name: UpdatePhoto :exec
UPDATE photos
SET title = ?, visibility = ?, gear_item_id = ?, content_type = ?, size_bytes = ?, status = ?
WHERE id = ?;

-- name: DeletePhotosByOwner :execresult
DELETE FROM photos WHERE owner_subject = ?;

-- name: DeletePhoto :exec
DELETE FROM photos WHERE id = ?;

-- name: ListStalePendingPhotos :many
-- 回収ジョブ: アップロードが完了しないまま放置された行
SELECT * FROM photos
WHERE status = 'pending_upload' AND created_at < ?
ORDER BY created_at LIMIT ?;
