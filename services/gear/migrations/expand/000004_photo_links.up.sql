-- 同期コマンド「使用機材の紐付け」の受け側（4.4、docs/02-architecture.md）。
-- photo からの POST /items/{id}:link-photo を冪等キーごと永続化する。
-- 結果（linked / rejected）を残すのは、photo の回収ジョブが**冪等キーで照会**して
-- pending を確定させるため——「受けたかどうか」を答えられなければ回収が成立しない。
CREATE TABLE photo_links (
    link_key   CHAR(32)        NOT NULL COMMENT 'photo が採番する冪等キー（Idempotency-Key）',
    item_id    BIGINT UNSIGNED NOT NULL COMMENT '紐付け先の機材',
    photo_id   BIGINT UNSIGNED NOT NULL COMMENT 'photo サービスの写真ID（値として持つだけ。FK は張らない）',
    status     VARCHAR(16)     NOT NULL COMMENT 'linked / rejected',
    reason     VARCHAR(100)    NOT NULL DEFAULT '' COMMENT 'rejected の理由（機械可読の短い語）',
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (link_key),
    KEY idx_photo_links_item (item_id, photo_id)
) COMMENT '写真と機材の紐付け（同期コマンドの受理記録。冪等キーが正）';
