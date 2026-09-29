-- name: GetItemDetail :one
SELECT id, kind, name, maker, created_at FROM items WHERE id = ?;

-- name: ListItems :many
SELECT id, kind, name, maker, created_at FROM items
ORDER BY id DESC
LIMIT ?;
