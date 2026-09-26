-- 生成物。手で編集しない。更新: dev/scripts/schema-dump.sh photo --write（マイグレーション全適用後の mysqldump --no-data）
CREATE TABLE `photos` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `owner_subject` varchar(255) NOT NULL,
  `visibility` enum('private','public') NOT NULL DEFAULT 'private',
  `gear_item_id` bigint unsigned DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  `object_key` varchar(255) DEFAULT NULL,
  `content_type` varchar(100) DEFAULT NULL,
  `size_bytes` bigint unsigned DEFAULT NULL,
  `status` enum('pending_upload','ready') NOT NULL DEFAULT 'ready',
  `title` varchar(1000) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_photos_owner` (`owner_subject`,`created_at`),
  KEY `idx_photos_status_created` (`status`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
