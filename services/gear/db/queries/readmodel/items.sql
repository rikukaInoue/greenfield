-- name: GetItemDetail :one
SELECT id, kind, name, maker, created_at FROM items WHERE id = ?;

-- name: ListItems :many
-- 一覧は行ごとに作例の件数を出す。photo へ行ごとに問い合わせると HTTP 越しの N+1 になるため、
-- ここだけ ReplicaView（photo_replica）を読む（internal-01 §ReplicaView の昇格条件）。
-- 同一 database 内の JOIN であり、他ドメインの**テーブル**を参照しているわけではない
-- （複製の正は photo。イベント契約にのみ依存する）。
SELECT i.id, i.kind, i.name, i.maker, i.created_at,
       COUNT(r.photo_id) AS photo_count
FROM items i
LEFT JOIN photo_replica r ON r.gear_item_id = i.id
GROUP BY i.id, i.kind, i.name, i.maker, i.created_at
ORDER BY i.id DESC
LIMIT ?;
