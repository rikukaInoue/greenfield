-- コマンド側（Entity の復元・保存）のクエリ。参照できるのは自ドメイン（photo）のテーブルのみ。

-- name: CreatePhoto :execresult
INSERT INTO photos (owner_subject, caption, visibility, gear_item_id)
VALUES (?, ?, ?, ?);

-- name: GetPhotoForUpdate :one
SELECT * FROM photos WHERE id = ? FOR UPDATE;
