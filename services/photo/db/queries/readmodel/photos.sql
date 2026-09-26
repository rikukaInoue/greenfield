-- 読み側（Read Model）のクエリ。Entity を経由せず応答の形を直接組み立てる。tx 外で実行する。
-- 参照できるのは自ドメイン（photo）のテーブルのみ。他ドメインは <name>-client 経由の HTTP か ReplicaView で取得する。

-- name: GetPhotoDetail :one
SELECT id, owner_subject, caption, visibility, gear_item_id, created_at, updated_at
FROM photos WHERE id = ?;

-- name: ListPhotosByIDs :many
-- 認可付き一覧: ListAccessible で得た ID 群を WHERE IN で絞る
SELECT id, owner_subject, caption, visibility, gear_item_id, created_at, updated_at
FROM photos WHERE id IN (sqlc.slice('ids')) ORDER BY created_at DESC;

-- name: ListPhotosByOwner :many
SELECT id, owner_subject, caption, visibility, gear_item_id, created_at, updated_at
FROM photos WHERE owner_subject = ? ORDER BY created_at DESC LIMIT ?;

-- name: ListPhotos :many
-- オペレータ向けの全件一覧
SELECT id, owner_subject, caption, visibility, gear_item_id, created_at, updated_at
FROM photos ORDER BY created_at DESC LIMIT ?;

-- name: ListPublicPhotosByGearItem :many
SELECT id, owner_subject, caption, visibility, gear_item_id, created_at, updated_at
FROM photos WHERE gear_item_id = ? AND visibility = 'public' ORDER BY created_at DESC LIMIT ?;
