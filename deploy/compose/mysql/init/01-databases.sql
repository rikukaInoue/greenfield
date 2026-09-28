-- database・ユーザー・GRANT（platform運用。サービスのマイグレーションには入れない）。
-- サービスごとに専用database + 2ユーザー:
--   <name>_app      : 自database の DML のみ（アプリ実行用）
--   <name>_migrate  : 自database の DDL + DML（migrate サブコマンド用）
-- 他サービスの database には SELECT 権限すら持たせない（規約をすり抜けたクエリは実行時に権限エラーで落ちる）。
-- scaffold が新サービス分をマーカーの前に追記する。

-- platform（authz 等の自作基盤。Phase 3）
CREATE DATABASE IF NOT EXISTS platform CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- localauthz（擬似ReBACのタプル置き場。Phase 3.2 で OpenFGA + authzサービスへ差し替える）。
-- サービスのDBとは別にすることで、本番同様「サービスのトランザクションに参加しない」状態を再現する
-- （業務側のロールバックでタプルは消えない = 孤児タプル #6 を再現できる）。
CREATE DATABASE IF NOT EXISTS localauthz CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS localauthz.relation_tuples (
    subject  VARCHAR(255) NOT NULL COMMENT 'user:<sub> / service:<client_id>',
    relation VARCHAR(64)  NOT NULL COMMENT 'owner / operator / ...',
    object   VARCHAR(255) NOT NULL COMMENT 'photo:<id> / platform:main',
    PRIMARY KEY (subject, relation, object),
    KEY idx_tuples_object (object, relation)
);
CREATE USER IF NOT EXISTS 'localauthz'@'%' IDENTIFIED BY 'localauthz';
GRANT SELECT, INSERT, UPDATE, DELETE ON localauthz.* TO 'localauthz'@'%';

-- photo
CREATE DATABASE IF NOT EXISTS photo CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER IF NOT EXISTS 'photo_app'@'%' IDENTIFIED BY 'photo_app';
CREATE USER IF NOT EXISTS 'photo_migrate'@'%' IDENTIFIED BY 'photo_migrate';
GRANT SELECT, INSERT, UPDATE, DELETE ON photo.* TO 'photo_app'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, LOCK TABLES ON photo.* TO 'photo_migrate'@'%';

-- gear
CREATE DATABASE IF NOT EXISTS gear CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER IF NOT EXISTS 'gear_app'@'%' IDENTIFIED BY 'gear_app';
CREATE USER IF NOT EXISTS 'gear_migrate'@'%' IDENTIFIED BY 'gear_migrate';
GRANT SELECT, INSERT, UPDATE, DELETE ON gear.* TO 'gear_app'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, LOCK TABLES ON gear.* TO 'gear_migrate'@'%';

-- OpenFGA（authzサービスの裏のタプルストア。Phase 3.2）。
-- サービスの database とは分離し、GRANT も openfga ユーザーに閉じる。
CREATE DATABASE IF NOT EXISTS openfga CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER IF NOT EXISTS 'openfga'@'%' IDENTIFIED BY 'openfga';
GRANT ALL PRIVILEGES ON openfga.* TO 'openfga'@'%';

-- scaffold:databases
FLUSH PRIVILEGES;
