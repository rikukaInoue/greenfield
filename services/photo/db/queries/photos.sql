-- 参照できるのは自ドメイン（photo）のテーブルのみ。他ドメインは <name>-client 経由の HTTP か ReplicaView で取得する。

-- name: CreatePhoto :execresult
INSERT INTO photos (owner_subject, caption, visibility, gear_item_id)
VALUES (?, ?, ?, ?);

-- name: GetPhoto :one
SELECT * FROM photos WHERE id = ?;

-- name: ListPhotosByIDs :many
-- 認可付き一覧: ListAccessible で得た ID 群を WHERE IN で絞る
SELECT * FROM photos WHERE id IN (sqlc.slice('ids')) ORDER BY created_at DESC;

-- name: ListPhotosByOwner :many
SELECT * FROM photos WHERE owner_subject = ? ORDER BY created_at DESC LIMIT ?;
