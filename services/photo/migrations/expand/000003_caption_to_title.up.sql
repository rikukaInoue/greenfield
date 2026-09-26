-- caption → title の改名（expand）。追加のみで、旧カラムはそのまま残す。
-- NULL 許容で追加するため INSTANT で通る。バックフィルはこのマイグレーションに含めず
-- バックグラウンドジョブで流す（デプロイ前ステップに長時間処理を置かない）。
ALTER TABLE photos
    ADD COLUMN title VARCHAR(1000) NULL,
    ALGORITHM = INSTANT;
