-- 機材（カタログ）。kind は投稿時に選ぶ分類で、後から増える前提の ENUM ではなく
-- VARCHAR + アプリ側検証にする（ENUM の変更は COPY になりうる。photo D-4 の教訓）。
CREATE TABLE items (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    kind       VARCHAR(32)  NOT NULL COMMENT 'camera / lens / tripod など。検証はアプリ側',
    name       VARCHAR(120) NOT NULL COMMENT '機種の表示名',
    maker      VARCHAR(120) NOT NULL DEFAULT '' COMMENT 'メーカー名（任意）',
    created_by VARCHAR(255) NOT NULL COMMENT '投稿者の subject',
    created_at DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_items_kind (kind, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
