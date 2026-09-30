#!/bin/sh
# #219: フェイルオーバー中の挙動を測るプローブ。mysql:8.4 コンテナ(one-off ECS タスク)で走る。
#
# 使い方: DBHOST=<host> DBPW=<pw> DURATION=<秒> sh failover-probe.sh
#
# 約 250ms 間隔で INSERT を打ち続け、1行1結果を出す:
#   PROBE|<epoch_ms>|ok|<latency_ms>
#   PROBE|<epoch_ms>|err|<latency_ms>|<エラー先頭>
# 1秒ごとに DNS の解決先も記録する(フリップの観測):
#   DNS|<epoch_ms>|<ip>
# 集計はログ回収後にローカルで行う(コンテナ内では素の1行を吐くだけにして、
# 失敗の瞬間の挙動を加工で失わない)。
set -u
AUTH="-h ${DBHOST} -P 3306 -uroot -p${DBPW} --connect-timeout=2"
DURATION="${DURATION:-420}"

# 1回目の実測での最重要発見: --connect-timeout は**確立済み**コネクションの凍結には
# 効かない。フェイルオーバーの瞬間は TCP がブラックホール化し、MySQL クライアントの
# read timeout は既定で無限なので、プローブ自体が10分以上ハングした。クライアント側の
# 締め切りは接続でなく**呼び出し全体**に掛ける(10.5 のタイムアウトヒエラルキーと同じ結論)。
q() { # q <deadline秒> <sql>
  timeout -k 1 "$1" mysql $AUTH -e "$2" 2>&1
  # rc=124 は timeout による強制終了
}

mysql $AUTH -e "CREATE DATABASE IF NOT EXISTS probe;
CREATE TABLE IF NOT EXISTS probe.beats (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);" 2>&1 | grep -v 'password on the command line'
echo "SETUP|$(date +%s%3N)|probe.beats ready|duration=${DURATION}s"

end=$(( $(date +%s) + DURATION ))
last_dns=0
while [ "$(date +%s)" -lt "$end" ]; do
  now=$(date +%s)
  if [ "$now" -gt "$last_dns" ]; then
    ip=$(getent hosts "$DBHOST" | awk '{print $1; exit}' 2>/dev/null || echo "resolve-fail")
    echo "DNS|$(date +%s%3N)|${ip:-none}"
    last_dns=$now
  fi
  s=$(date +%s%3N)
  raw=$(q 3 "INSERT INTO probe.beats () VALUES ();")
  rc=$?
  out=$(printf '%s' "$raw" | grep -v 'password on the command line' | head -1)
  e=$(date +%s%3N)
  if [ -z "$out" ] && [ "$rc" -eq 0 ]; then
    echo "PROBE|${e}|ok|$((e - s))"
  elif [ "$rc" -eq 124 ] || [ -z "$out" ]; then
    echo "PROBE|${e}|err|$((e - s))|client-deadline(3s)"
  else
    echo "PROBE|${e}|err|$((e - s))|${out}"
  fi
  sleep 0.25
done
n=$(q 10 "SELECT COUNT(*) FROM probe.beats;" 2>/dev/null | tail -1 || echo "?")
echo "DONE|$(date +%s%3N)|rows=${n}"
