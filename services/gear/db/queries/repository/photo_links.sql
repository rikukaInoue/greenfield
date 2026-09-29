-- 同期コマンド「使用機材の紐付け」の受理記録（4.4）。

-- name: InsertPhotoLink :execresult
-- 冪等: 同じ link_key の再送は INSERT IGNORE が弾き（RowsAffected=0）、
-- 呼び出し側は既存の結果を返す。UPDATE しないのが要点——リトライで結果が変わると
-- 「1回目 rejected、2回目 linked」のような不定になる
INSERT IGNORE INTO photo_links (link_key, item_id, photo_id, status, reason)
VALUES (?, ?, ?, ?, ?);

-- name: GetPhotoLink :one
SELECT link_key, item_id, photo_id, status, reason FROM photo_links WHERE link_key = ?;

-- name: ItemExists :one
SELECT EXISTS(SELECT 1 FROM items WHERE id = ?);
