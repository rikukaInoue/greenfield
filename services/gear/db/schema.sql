-- 生成物。手で編集しない。更新: dev/scripts/schema-dump.sh gear --write（マイグレーション全適用後の mysqldump --no-data）
CREATE TABLE `inbox` (
  `event_id` char(32) NOT NULL COMMENT '送信側が採番したイベントID',
  `event_type` varchar(100) NOT NULL,
  `received_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`event_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='処理済みイベント。業務処理と同一 tx で記録し、重複配送を無害化する';
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
CREATE TABLE `photo_links` (
  `link_key` char(32) NOT NULL COMMENT 'photo が採番する冪等キー（Idempotency-Key）',
  `item_id` bigint unsigned NOT NULL COMMENT '紐付け先の機材',
  `photo_id` bigint unsigned NOT NULL COMMENT 'photo サービスの写真ID（値として持つだけ。FK は張らない）',
  `status` varchar(16) NOT NULL COMMENT 'linked / rejected',
  `reason` varchar(100) NOT NULL DEFAULT '' COMMENT 'rejected の理由（機械可読の短い語）',
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`link_key`),
  KEY `idx_photo_links_item` (`item_id`,`photo_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='写真と機材の紐付け（同期コマンドの受理記録。冪等キーが正）';
CREATE TABLE `photo_replica` (
  `photo_id` bigint unsigned NOT NULL COMMENT 'photo サービスの ID（正は相手）',
  `gear_item_id` bigint unsigned NOT NULL COMMENT 'どの機材の作例か',
  `caption` varchar(1000) NOT NULL,
  `photo_created_at` datetime(6) NOT NULL COMMENT 'photo 側の作成時刻（並び順に使う）',
  `replicated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`photo_id`),
  KEY `idx_photo_replica_item` (`gear_item_id`,`photo_created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='他ドメイン（photo）の公開データの読み取り専用の複製。表示専用';
