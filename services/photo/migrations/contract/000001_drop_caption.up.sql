-- contract。前提の2ゲートを満たしてから実行する:
--   1. sqlc の参照がゼロ（クエリから caption を外し、再生成が通ること）
--   2. release.photo_caption_to_title が削除済み（フラグと旧読み経路がコードから消えていること）
ALTER TABLE photos DROP COLUMN caption, ALGORITHM = INSTANT;
