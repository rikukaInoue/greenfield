-- 画像本体はオブジェクトストレージに置き、DB はその鍵と状態だけを持つ。
-- status は既存行を見えるままにするため DEFAULT 'ready'。新規挿入は必ず明示的に指定する。
ALTER TABLE photos
    ADD COLUMN object_key   VARCHAR(255) NULL,
    ADD COLUMN content_type VARCHAR(100) NULL,
    ADD COLUMN size_bytes   BIGINT UNSIGNED NULL,
    ADD COLUMN status       ENUM('pending_upload', 'ready') NOT NULL DEFAULT 'ready',
    ALGORITHM = INSTANT;

-- 回収ジョブが pending のまま残った行を古い順に拾う
CREATE INDEX idx_photos_status_created ON photos (status, created_at);
