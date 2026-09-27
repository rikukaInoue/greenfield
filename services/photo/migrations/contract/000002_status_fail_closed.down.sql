-- down は形式上の維持（本番では前方修正）。2) の UPDATE は戻さない —
-- 「ready なのに実体が無い」へ戻すことに意味は無い。
ALTER TABLE photos DROP CONSTRAINT chk_photos_ready_has_object;
ALTER TABLE photos ALTER COLUMN status SET DEFAULT 'ready', ALGORITHM = INSTANT;
