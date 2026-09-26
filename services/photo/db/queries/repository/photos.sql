-- コマンド側（Entity の復元・保存）のクエリ。参照できるのは自ドメイン（photo）のテーブルのみ。

-- name: CreatePhoto :execresult
INSERT INTO photos (owner_subject, caption, visibility, gear_item_id)
VALUES (?, ?, ?, ?);

-- name: GetPhotoForUpdate :one
SELECT * FROM photos WHERE id = ? FOR UPDATE;

-- name: UpdatePhoto :exec
UPDATE photos SET caption = ?, visibility = ?, gear_item_id = ? WHERE id = ?;

-- name: DeletePhotosByOwner :execresult
DELETE FROM photos WHERE owner_subject = ?;
