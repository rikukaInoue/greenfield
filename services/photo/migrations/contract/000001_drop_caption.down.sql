-- down は形式上維持するが本番では使用しない
ALTER TABLE photos ADD COLUMN caption VARCHAR(1000) NOT NULL DEFAULT '';
