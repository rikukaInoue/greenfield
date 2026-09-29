-- Eventual（outbox + relay）の導入（4.2、internal-03 §2.3）。
-- core/consistency/ddl/outbox.sql のテンプレートを複写したもの。テンプレート更新時は
-- 追随マイグレーションを取り込む（internal-05）。
-- core/consistency（Outbox.Publish / Relay）が列名を前提に読み書きする。
CREATE TABLE outbox (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id        CHAR(32)        NOT NULL COMMENT '送信側が採番。受信側 inbox とバスの MessageDeduplicationId で使う',
    event_type      VARCHAR(100)    NOT NULL COMMENT '例: photo.published',
    aggregate_id    VARCHAR(255)    NOT NULL COMMENT '例: photo:123。バスの MessageGroupId（集約内順序）',
    payload         JSON            NOT NULL,
    created_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    published_at    DATETIME(6)     NULL COMMENT 'NULL = 未送信。relay が送信成功後に記録',
    attempts        INT             NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(6)     NULL COMMENT '指数バックオフの次回試行時刻',
    last_error      TEXT            NULL COMMENT '上限超過はエラー状態として残す（削除しない。監視対象）',
    PRIMARY KEY (id),
    UNIQUE KEY uq_outbox_event (event_id),
    KEY idx_outbox_unpublished (published_at, id)
) COMMENT '送信予定のイベント。業務データと同一 tx で INSERT（再生の正でもある）';
