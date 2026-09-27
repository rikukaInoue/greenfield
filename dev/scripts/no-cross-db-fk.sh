#!/usr/bin/env bash
# database を跨ぐ外部キーが無いことを確かめる（#136）。
#
# internal-05 は「参照できるのは自ドメインのテーブルのみ」と定め、§38 は
# 「モジュール間のマイグレーションは完全に独立並行できる」と言い切っている。
# **跨ぐ FK が 1 本あるとその主張が崩れる。** 実測(MySQL 8.4):
#
#   親(gear)の重い DDL 中に 子(photo)へ DDL  → ERROR 1205 Lock wait timeout exceeded
#   親(gear)の DDL 中に 子(photo)へ INSERT    → 成功
#
# 親テーブルの DDL は FK 定義の整合のために子テーブルのメタデータロックも取るので、
# ロックがサービス境界を越える。**壊れるのは DDL = デプロイだけで通常の読み書きは通る**ため、
# 日常では気づけない。気づくのは「なぜか他サービスのデプロイが 1205 で落ちる」時点。
#
# MySQL は `REFERENCES other_db.tbl(id)` をエラーにも警告にもしない。文章で禁じたものは
# 機械でも禁じる、がこのスクリプトの存在理由。
#
# 同一 database 内の FK は**止めない**。集約内の不変条件として正当で、むしろ張るべき場合がある。
#
#   dev/scripts/no-cross-db-fk.sh            # 実行時の DB を見る
#   MYSQL_CONTAINER=... MYSQL_PORT=...       # schema-dump.sh と同じ流儀で接続先を差せる
set -euo pipefail

container=${MYSQL_CONTAINER-greenfield-mysql}
host=${MYSQL_HOST:-127.0.0.1}
port=${MYSQL_PORT:-3306}

if [ -n "$container" ]; then
  mysql_root() { docker exec -i "$container" mysql -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
else
  mysql_root() { mysql -h "$host" -P "$port" -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
fi

# information_schema は「いま DB にある姿」を返す。マイグレーションのファイルを読む形にしないのは、
# 適用結果（= 実際にロックを取る側）を見たいため。
#
# 見るのは key_column_usage。**referential_constraints には referenced_table_schema が無い**
# （あるのは unique_constraint_schema で、これは参照先の一意制約のスキーマ）。
# 最初に referential_constraints で書いたら ERROR 1054 になり、stderr を捨てていたので
# 「跨ぐ FK は無い」と読み違えた。空の結果と失敗を区別できない書き方をしない。
rows=$(mysql_root -N -B -e "
  SELECT CONCAT(table_schema, '.', table_name, ' -> ',
                referenced_table_schema, '.', referenced_table_name,
                '  (', constraint_name, ')')
  FROM information_schema.key_column_usage
  WHERE referenced_table_schema IS NOT NULL
    AND table_schema <> referenced_table_schema
  ORDER BY table_schema, table_name;")

if [ -z "$rows" ]; then
  echo "database を跨ぐ外部キーは無い"
  exit 0
fi

cat >&2 <<MSG
database を跨ぐ外部キーがある（internal-05 違反）:

$rows

これがあると、親テーブルの DDL が子テーブルのメタデータロックを取るため、
**他サービスのデプロイが ERROR 1205 で落ちる**（通常の読み書きは通るので日常では気づけない）。

直し方: 他ドメインのテーブルを参照せず、**ID を値として持つ**。
存在保証が要るなら <name>-client 経由の HTTP か ReplicaView で解決し、
即時の整合が要るなら pending 状態 + 同期コマンド + 冪等キー（internal-03）にする。
MSG
exit 1
