-- 生成物。手で編集しない。更新: dev/scripts/schema-dump.sh photo --write（マイグレーション全適用後の mysqldump --no-data）
CREATE TABLE `outbox` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `event_id` char(32) NOT NULL COMMENT '送信側が採番。受信側 inbox とバスの MessageDeduplicationId で使う',
  `event_type` varchar(100) NOT NULL COMMENT '例: photo.published',
  `aggregate_id` varchar(255) NOT NULL COMMENT '例: photo:123。バスの MessageGroupId（集約内順序）',
  `payload` json NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `published_at` datetime(6) DEFAULT NULL COMMENT 'NULL = 未送信。relay が送信成功後に記録',
  `attempts` int NOT NULL DEFAULT '0',
  `next_attempt_at` datetime(6) DEFAULT NULL COMMENT '指数バックオフの次回試行時刻',
  `last_error` text COMMENT '上限超過はエラー状態として残す（削除しない。監視対象）',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_outbox_event` (`event_id`),
  KEY `idx_outbox_unpublished` (`published_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='送信予定のイベント。業務データと同一 tx で INSERT（再生の正でもある）';
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
  `status` enum('pending_upload','ready') NOT NULL DEFAULT 'pending_upload',
  `title` varchar(1000) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_photos_owner` (`owner_subject`,`created_at`),
  KEY `idx_photos_status_created` (`status`,`created_at`),
  CONSTRAINT `chk_photos_ready_has_object` CHECK (((`status` <> _utf8mb4'ready') or (`object_key` is not null)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
