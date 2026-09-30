-- photo の公開データの ReplicaView（4.3、internal-01 §ReplicaView）。
-- 正は photo にあり、イベント購読（photo.published / photo.deleted）で追従する。
-- **表示専用**であり業務判断に使わない（usecase / domain からの import は depguard が止める）。
--
-- 持つのは表示に要る最小限だけ。相手のテーブルを丸ごと写さない——依存してよいのは
-- photo が公開すると決めたイベントの契約であって、photo のテーブル構造ではない。
-- 署名付きURLは期限付きで複製できないため持たない（鮮度が要る詳細は HTTP のまま。4.1）。
CREATE TABLE photo_replica (
    photo_id     BIGINT UNSIGNED NOT NULL COMMENT 'photo サービスの ID（正は相手）',
    gear_item_id BIGINT UNSIGNED NOT NULL COMMENT 'どの機材の作例か',
    caption      VARCHAR(1000)   NOT NULL,
    photo_created_at DATETIME(6) NOT NULL COMMENT 'photo 側の作成時刻（並び順に使う）',
    replicated_at    DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (photo_id),
    KEY idx_photo_replica_item (gear_item_id, photo_created_at)
) COMMENT '他ドメイン（photo）の公開データの読み取り専用の複製。表示専用';
