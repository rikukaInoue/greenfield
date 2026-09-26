-- 写真投稿。所有者は Principal.Subject（不透明な識別子）。gear_item_id は gear の item ID のみ保持する（相手のテーブルは参照しない）。
CREATE TABLE photos (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    owner_subject VARCHAR(255)    NOT NULL,
    caption       VARCHAR(1000)   NOT NULL DEFAULT '',
    visibility    ENUM('private', 'public') NOT NULL DEFAULT 'private',
    gear_item_id  BIGINT UNSIGNED NULL,
    created_at    DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_photos_owner (owner_subject, created_at)
);
