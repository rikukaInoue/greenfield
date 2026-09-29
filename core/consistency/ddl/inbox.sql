-- inbox（受信側の重複排除。internal-03 §2.3）の DDL テンプレート。
-- 物理テーブルはサービスごとに持ち、共有しない（internal-05）。
-- バス（SQS FIFO）の重複排除は5分窓の補助であり、inbox が主たる防御。
--
-- core/consistency（Inbox.MarkProcessed）はこの列名を前提に書く:
--   event_id, event_type, received_at
CREATE TABLE inbox (
    event_id    CHAR(32)     NOT NULL COMMENT '送信側が採番したイベントID',
    event_type  VARCHAR(100) NOT NULL,
    received_at DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (event_id)
) COMMENT '処理済みイベント。業務処理と同一 tx で記録し、重複配送を無害化する';
