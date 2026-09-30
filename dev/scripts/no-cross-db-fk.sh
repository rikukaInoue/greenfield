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
#   dev/scripts/no-cross-db-fk.sh photo gear  # この database が全部ある状態で検査する
#   MYSQL_CONTAINER=... MYSQL_PORT=...        # schema-dump.sh と同じ流儀で接続先を差せる
#
# モード（FK_CHECK_MODE、既定 enforce）:
#   enforce: 跨ぐ FK が 1 本でもあれば失敗する。グリーンフィールドの規律（既定）
#   warn:    失敗させず**棚卸しを出す**。モノリスからの移行途中に「あとどのペアの FK が
#            切れれば独立デプロイできるか」をシグナルするためのモード（#197）。
#            FK_BASELINE=<file> を渡すと git 管理のベースラインと突き合わせ、
#            **増加（逆行）だけを失敗**にする。減少は進捗として報告する
#
# **検査対象の database 名を必ず渡す。** 跨ぐ FK は定義上 2 つ以上の database が要るので、
# 1 つしか無い DB に対して実行すると「跨ぐ FK は無い」が必ず返る。最初はこれをモジュール毎の
# CI ジョブ（MODULES で 1 サービスに絞られる）に置いてしまい、**構造的に何も検出できない検査**
# になっていた。引数を必須にして、前提が崩れていたら検査自体を失敗させる。
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <database> <database> [...]  （跨ぐ FK を検出するには 2 つ以上必要）" >&2
  exit 2
fi
want=("$@")

container=${MYSQL_CONTAINER-greenfield-mysql}
host=${MYSQL_HOST:-127.0.0.1}
port=${MYSQL_PORT:-13306}

if [ -n "$container" ]; then
  mysql_root() { docker exec -i "$container" mysql -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
else
  mysql_root() { mysql -h "$host" -P "$port" -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
fi

# 検査の前提を先に確かめる。見るのは **database の存在ではなくテーブルの有無**。
# database は compose の init SQL（prepare も同じものを流す）が最初に全サービス分作るので、
# 「database がある」は MODULES で 1 サービスに絞った状態でも成立してしまう。
# 跨ぐ FK は参照先のテーブルが無いと張れない = テーブルが揃っていない DB では検出できない。
# 検査は「通った」ではなく「何を見た上で通ったか」まで言えないと意味がない。
empty=()
for db in "${want[@]}"; do
  n=$(mysql_root -N -B -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = '$db';")
  [ "$n" -gt 0 ] || empty+=("$db")
done
if [ "${#empty[@]}" -gt 0 ]; then
  cat >&2 <<MSG
検査の前提が崩れている: テーブルが 1 つも無い database: ${empty[*]}

跨ぐ FK は参照先のテーブルが無いと張れないので、この状態では**何も検出できない**。
全サービスのマイグレーションを適用してから実行する（MODULES で絞ると 1 サービス分しか流れない）。
MSG
  exit 1
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

mode=${FK_CHECK_MODE:-enforce}
case "$mode" in enforce|warn) ;; *) echo "NG: FK_CHECK_MODE=${mode}（enforce か warn）" >&2; exit 2 ;; esac

if [ -z "$rows" ]; then
  echo "database を跨ぐ外部キーは無い（見た database: ${want[*]}、モード: ${mode}）"
  if [ "$mode" = warn ] && [ -n "${FK_BASELINE:-}" ] && [ -s "$FK_BASELINE" ]; then
    echo "進捗: ベースラインの跨ぎ FK は全て解消済み。ベースラインを空にして enforce へ移行できる"
  fi
  exit 0
fi

if [ "$mode" = warn ]; then
  # 棚卸し: サービスペアごとに集計し、「このペアが 0 になれば独立」を出す
  echo "database を跨ぐ外部キーの棚卸し（警告モード。見た database: ${want[*]}）:"
  echo
  echo "$rows"
  echo
  echo "ペア別（この本数が 0 になれば、そのペア間は独立デプロイ可能）:"
  echo "$rows" | awk -F'[. ]' '{ pairs[$1 " -> " $4]++ } END { for (p in pairs) printf "  %-24s 残り %d 本\n", p, pairs[p] }' | sort
  if [ -n "${FK_BASELINE:-}" ]; then
    if [ ! -f "$FK_BASELINE" ]; then
      echo "NG: FK_BASELINE=$FK_BASELINE が無い。現状を固定するには:" >&2
      echo "  FK_CHECK_MODE=warn $0 ${want[*]} | grep ' -> ' > $FK_BASELINE" >&2
      exit 1
    fi
    new=$(comm -13 <(sort "$FK_BASELINE") <(echo "$rows" | sort))
    gone=$(comm -23 <(sort "$FK_BASELINE") <(echo "$rows" | sort))
    if [ -n "$gone" ]; then
      echo
      echo "進捗: ベースラインから減った FK（ベースラインの更新を推奨）:"
      echo "$gone"
    fi
    if [ -n "$new" ]; then
      cat >&2 <<MSG

NG: ベースラインに無い跨ぎ FK が**増えている**（移行の逆行）:

$new
MSG
      exit 1
    fi
  fi
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
移行途中のコードベースで進捗を測るには FK_CHECK_MODE=warn（ヘッダのコメント参照）。
MSG
exit 1
