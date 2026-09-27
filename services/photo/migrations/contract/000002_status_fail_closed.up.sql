-- contract。status の既定を fail-open から fail-closed に変え、「ready なのに実体が無い」を
-- 構造で禁じる（監査 D-4 / #122）。
--
-- 前提のゲート: status を指定しない INSERT がコードに無いこと。
-- `CreatePhoto` は status を明示しており、他に photos へ INSERT する経路は無い。
--
-- 000002_photo_objects が DEFAULT 'ready' にしたのは、カラム追加時に既存行を
-- 見えるままにするためで、その役目は済んでいる。既定が 'ready' のままだと、
-- status を忘れた INSERT が即座に一覧へ出て、しかも CommitUpload（pending でないと拒否）も
-- reclaim（pending しか見ない）も直せない = 回復経路が全部塞がった行ができる。

-- 1) 既定を fail-closed に。
--    実測（MySQL 8.4.11）: ALGORITHM = INSTANT で通る（メタデータのみ）。
--    既存行の値は変わらない（DEFAULT は以後の INSERT にしか効かない）。
ALTER TABLE photos ALTER COLUMN status SET DEFAULT 'pending_upload', ALGORITHM = INSTANT;

-- 2) CHECK の前提を満たす。違反行が1行でもあると 3) の ALTER が失敗する。
--    これはバックフィル（リリースに同梱しない規約）ではなく、制約を付けるための修復で、
--    対象は「壊れた行」だけ＝通常ゼロ。pending_upload へ落とすと reclaim が拾えるようになる
--    （ready のままだとどのコード経路からも手が出せない）。
UPDATE photos SET status = 'pending_upload' WHERE status = 'ready' AND object_key IS NULL;

-- 3) 構造で縛る。
--    実測（MySQL 8.4.11）: INSTANT も INPLACE も不可（ERROR 1845）、**COPY のみ**。
--    internal-05 の「どちらも通らない変更は例外として個別計画する」に該当するため、
--    方式を明示する。greenfield の行数では即時に終わるが、本番規模の photos に当てる際は
--    gh-ost / pt-online-schema-change か保守時間を計画すること（検証ログに記録済み）。
ALTER TABLE photos
    ADD CONSTRAINT chk_photos_ready_has_object
        CHECK (status <> 'ready' OR object_key IS NOT NULL),
    ALGORITHM = COPY, LOCK = SHARED;
