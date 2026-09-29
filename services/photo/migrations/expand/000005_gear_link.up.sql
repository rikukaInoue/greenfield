-- 同期コマンド「使用機材の紐付け」の送り側の状態（4.4、docs/02-architecture.md）。
-- gear_item_id は 2.2 から「値を保存するだけ」だったが、ここから紐付けの確定状態を持つ:
--   NULL     = 紐付け要求なし（機材未指定）
--   pending  = Atomic で確定済み・gear の結果待ち（gear 停止中はここに留まる）
--   linked   = gear が受理
--   rejected = gear が拒否（機材が存在しない等）
-- gear_link_key は photo が採番する冪等キー。回収ジョブはこのキーで gear に照会する。
-- どちらも ALGORITHM=INSTANT で入る列追加。
ALTER TABLE photos
    ADD COLUMN gear_link_status VARCHAR(16) NULL COMMENT 'pending / linked / rejected（NULL=要求なし）',
    ADD COLUMN gear_link_key CHAR(32) NULL COMMENT '紐付けコマンドの冪等キー（photo が採番）';

-- pending の回収スキャン用。ALTER とは別文（INSTANT にならない操作を混ぜない）
ALTER TABLE photos ADD KEY idx_photos_gear_link (gear_link_status, id);
