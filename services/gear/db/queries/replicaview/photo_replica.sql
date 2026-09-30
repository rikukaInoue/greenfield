-- ReplicaView（photo の公開データの複製）の書き込みと再構築。
-- 読み取りは readmodel 側のクエリが JOIN で行う（表示は Read Model の役目）。

-- name: UpsertPhotoReplica :exec
-- 同一イベントの再適用が無害であること（自然冪等）を SQL 側でも担保する。
-- inbox が主たる防御だが、再構築（replay）は inbox を空にしてから流すため、
-- ここが冪等でないと再構築のたびに重複キーで落ちる
INSERT INTO photo_replica (photo_id, gear_item_id, caption, photo_created_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    gear_item_id = VALUES(gear_item_id),
    caption = VALUES(caption),
    photo_created_at = VALUES(photo_created_at);

-- name: DeletePhotoReplica :exec
-- 存在しない行の削除も無害（再配送・再生で複数回届く前提）
DELETE FROM photo_replica WHERE photo_id = ?;

-- name: TruncatePhotoReplica :exec
-- 再構築の起点。管理コマンドからのみ呼ぶ
DELETE FROM photo_replica;

-- name: CountPhotoReplica :one
SELECT COUNT(*) FROM photo_replica;

-- name: ChecksumPhotoReplica :one
-- 再構築結果の一致を機械で比べるための指紋（check #10）。
-- replicated_at は再構築で必ず変わるので含めない——**変わってよい列を含めると
-- 「一致」が絶対に成立しない検査になる**
SELECT COALESCE(MD5(GROUP_CONCAT(
    CONCAT_WS(':', photo_id, gear_item_id, caption, photo_created_at)
    ORDER BY photo_id SEPARATOR '|')), '') AS fingerprint
FROM photo_replica;
