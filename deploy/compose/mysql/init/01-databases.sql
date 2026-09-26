-- database・ユーザー・GRANT（platform運用。サービスのマイグレーションには入れない）。
-- サービスごとに専用database + 2ユーザー:
--   <name>_app      : 自database の DML のみ（アプリ実行用）
--   <name>_migrate  : 自database の DDL + DML（migrate サブコマンド用）
-- 他サービスの database には SELECT 権限すら持たせない（規約をすり抜けたクエリは実行時に権限エラーで落ちる）。
-- scaffold が新サービス分をマーカーの前に追記する。

-- platform（authz 等の自作基盤。Phase 3）
CREATE DATABASE IF NOT EXISTS platform CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- photo
CREATE DATABASE IF NOT EXISTS photo CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER IF NOT EXISTS 'photo_app'@'%' IDENTIFIED BY 'photo_app';
CREATE USER IF NOT EXISTS 'photo_migrate'@'%' IDENTIFIED BY 'photo_migrate';
GRANT SELECT, INSERT, UPDATE, DELETE ON photo.* TO 'photo_app'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, LOCK TABLES ON photo.* TO 'photo_migrate'@'%';

-- scaffold:databases
FLUSH PRIVILEGES;
