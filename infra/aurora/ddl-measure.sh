#!/bin/sh
# 7.4(#65): INSTANT DDL の判定と所要時間を測る。mysql クライアントだけで動く
# (one-off ECS タスクの mysql:8.4 コンテナ内でも、手元の docker exec でも同じに走る)。
#
# 使い方: DBHOST=<host> DBPORT=<port> DBPW=<password> sh ddl-measure.sh
#
# 判定の見方: rc=0 で成功。ALGORITHM を明示しているので、
# その方式で**通るか通らないか**が結果そのもの(エラー 1845 = その方式は不可)。
set -u
AUTH="-h ${DBHOST} -P ${DBPORT:-3306} -uroot -p${DBPW} --connect-timeout=10"

run() {
  desc="$1"; sql="$2"
  s=$(date +%s%N)
  out=$(mysql $AUTH ddl_lab -e "$sql" 2>&1); rc=$?
  e=$(date +%s%N)
  # パスワード警告は結果でないので落とす
  out=$(printf '%s' "$out" | grep -v 'password on the command line' | head -2 | tr '\n' ' ')
  echo "RESULT|${desc}|rc=${rc}|$(( (e - s) / 1000000 ))ms|${out}"
}

echo "== seed: ddl_lab.photos_like に 2,097,152 行 =="
mysql $AUTH -e "DROP DATABASE IF EXISTS ddl_lab; CREATE DATABASE ddl_lab;" 2>&1 | grep -v password
mysql $AUTH ddl_lab -e "
CREATE TABLE photos_like (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  status VARCHAR(32) NOT NULL DEFAULT 'ready',
  object_key VARCHAR(255) NULL,
  caption VARCHAR(1000) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO photos_like (status, object_key, caption)
VALUES ('ready', 'k/0001', 'seed'), ('pending_upload', NULL, 'seed');" 2>&1 | grep -v password
s=$(date +%s)
i=0
while [ $i -lt 20 ]; do
  mysql $AUTH ddl_lab -e \
    "INSERT INTO photos_like (status, object_key, caption)
     SELECT status, object_key, caption FROM photos_like;" 2>&1 | grep -v password
  i=$((i + 1))
done
echo "seed took $(( $(date +%s) - s ))s, rows: $(mysql $AUTH -N ddl_lab -e 'SELECT COUNT(*) FROM photos_like;' 2>/dev/null)"

echo "== 計測(リポジトリ実物の expand / contract の DDL と同型) =="
# expand 000003/000005 と同型: NULL 許容の列追加
run "ADD COLUMN (INSTANT)" \
  "ALTER TABLE photos_like ADD COLUMN title VARCHAR(1000) NULL, ALGORITHM=INSTANT;"
# expand 000005 と同型: 別文でのインデックス追加(INSTANT にならない操作を混ぜない規約)
run "ADD KEY (INPLACE)" \
  "ALTER TABLE photos_like ADD KEY idx_status_id (status, id), ALGORITHM=INPLACE;"
# contract 000002 の 1) と同型: 既定値の変更
run "SET DEFAULT (INSTANT)" \
  "ALTER TABLE photos_like ALTER COLUMN status SET DEFAULT 'pending_upload', ALGORITHM=INSTANT;"
# contract 000002 の 3) と同型: CHECK 制約の追加。ローカル 8.4.11 の実測は
# INSTANT / INPLACE とも不可(ERROR 1845)で COPY のみ。Aurora で同じかがこの計測の核
run "ADD CHECK (INSTANT)" \
  "ALTER TABLE photos_like ADD CONSTRAINT chk_ready_has_object CHECK (status <> 'ready' OR object_key IS NOT NULL), ALGORITHM=INSTANT;"
run "ADD CHECK (INPLACE)" \
  "ALTER TABLE photos_like ADD CONSTRAINT chk_ready_has_object CHECK (status <> 'ready' OR object_key IS NOT NULL), ALGORITHM=INPLACE;"
run "ADD CHECK (COPY, LOCK=SHARED)" \
  "ALTER TABLE photos_like ADD CONSTRAINT chk_ready_has_object CHECK (status <> 'ready' OR object_key IS NOT NULL), ALGORITHM=COPY, LOCK=SHARED;"
# 8.0.29+ / 8.4: DROP COLUMN も INSTANT 対象
run "DROP COLUMN (INSTANT)" \
  "ALTER TABLE photos_like DROP COLUMN title, ALGORITHM=INSTANT;"

echo "== version =="
mysql $AUTH -N -e "SELECT VERSION(), @@aurora_version;" 2>/dev/null \
  || mysql $AUTH -N -e "SELECT VERSION(), 'not-aurora';" 2>/dev/null
echo "DONE"
