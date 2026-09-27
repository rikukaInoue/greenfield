-- 読み側（Read Model）のクエリ。Entity を経由せず応答の形を直接組み立てる。tx 外で実行する。
-- 参照できるのは自ドメイン（photo）のテーブルのみ。他ドメインは <name>-client 経由の HTTP か ReplicaView で取得する。
-- アップロード未完了（pending_upload）の行は表示経路に出さない。
-- SELECT * を使うのは全列を表示に使うため。contract で列を落とせば生成コードが変わり、参照側がコンパイルで落ちる。
-- 改名は完了済み。表示は title だけを使う。

-- name: GetPhotoDetail :one
-- 他の表示クエリと同じく pending_upload は返さない。詳細だけ status を見ていなかったため、
-- 画像を上げていない写真に 200 が返り、**存在しないオブジェクトへ署名付き URL を発行**していた
-- （ADR 0009 が「有害」と書いた状態そのもの）。オペレータが未完了の行を見るのは一覧（ListPhotos）の役目。
SELECT * FROM photos WHERE id = ? AND status = 'ready';

-- name: ListPhotosByIDs :many
-- 認可付き一覧: ListAccessible で得た ID 群を WHERE IN で絞る
SELECT * FROM photos WHERE id IN (sqlc.slice('ids')) AND status = 'ready' ORDER BY created_at DESC;

-- name: ListPhotosByOwner :many
SELECT * FROM photos WHERE owner_subject = ? AND status = 'ready' ORDER BY created_at DESC LIMIT ?;

-- name: ListPhotos :many
-- オペレータ向けの全件一覧（pending も含める。運用上の可視性のため）
SELECT * FROM photos ORDER BY created_at DESC LIMIT ?;

-- name: ListPublicPhotosByGearItem :many
SELECT * FROM photos WHERE gear_item_id = ? AND visibility = 'public' AND status = 'ready' ORDER BY created_at DESC LIMIT ?;
