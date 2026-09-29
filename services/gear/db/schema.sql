-- 生成物。手で編集しない。更新: dev/scripts/schema-dump.sh gear --write（マイグレーション全適用後の mysqldump --no-data）
CREATE TABLE `items` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `kind` varchar(32) NOT NULL COMMENT 'camera / lens / tripod など。検証はアプリ側',
  `name` varchar(120) NOT NULL COMMENT '機種の表示名',
  `maker` varchar(120) NOT NULL DEFAULT '' COMMENT 'メーカー名（任意）',
  `created_by` varchar(255) NOT NULL COMMENT '投稿者の subject',
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_items_kind` (`kind`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
