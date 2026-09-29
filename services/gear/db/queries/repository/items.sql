-- name: CreateItem :execresult
INSERT INTO items (kind, name, maker, created_by) VALUES (?, ?, ?, ?);

-- name: GetItem :one
SELECT id, kind, name, maker, created_by, created_at FROM items WHERE id = ?;
