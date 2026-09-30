#!/usr/bin/env bash
# 排水(グレースフルシャットダウン)のドリル(10.8 / #222)。
#
# 確かめること:
#   A. serve: SIGTERM 後も処理中のリクエストは完走する(全部 201)。排水完了ログが出る
#   B. serve: SIGKILL だと処理中が切断される(= A が守っているものの実証。破ってみせる)
#   C. relay: SIGTERM で処理中のバッチを完走させ、context canceled を出さず、
#      未送信の残数を記録して exit 0 で止まる
#   D. consumer: SIGTERM で受信済みを処理しきってから止まる。全量ドレイン後、
#      inbox は全件・重複スキップ 0(排水が自作の再配信を作らないこと)
#
# 前提: mysql(13306) / localstack(4566) 起動済み、photo / gear マイグレーション適用済み。
set -uo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

MYSQL="${MYSQL_CMD:-mysql -h 127.0.0.1 -P 13306 -uroot -proot}"
LOCALSTACK="${LOCALSTACK_CONTAINER:-greenfield-localstack}"
export AWS_ENDPOINT_URL="${AWS_ENDPOINT_URL:-http://localhost:4566}"
export AWS_REGION="${AWS_REGION:-us-east-1}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-testtest}"
export ENV=dev

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

tmp=$(mktemp -d)
pids=""
cleanup() {
  for p in ${pids}; do kill -9 "$p" 2>/dev/null || true; done
  ${MYSQL} photo -e "DELETE FROM outbox WHERE aggregate_id LIKE 'photo:95%';" >/dev/null 2>&1
  ${MYSQL} gear  -e "DELETE FROM inbox  WHERE event_type = 'photo.drain.check';" >/dev/null 2>&1
  rm -rf "$tmp"
}
trap cleanup EXIT

PHOTO_BIN="$tmp/photo"; ( cd services/photo && GOWORK=off go build -o "$PHOTO_BIN" ./cmd/photo ) || exit 1
GEAR_BIN="$tmp/gear";   ( cd services/gear  && GOWORK=off go build -o "$GEAR_BIN" ./cmd/gear )   || exit 1
start_bg() { # start_bg <logfile> <env...> <cmd...> — exec 起動で kill が本体に届く
  local log=$1; shift
  ( exec env "$@" > "$log" 2>&1 ) &
  last_pid=$!
  pids="$pids $last_pid"
}

TOKEN=$( (cd dev && go run ./devtoken --user alice) 2>/dev/null | tail -1)
[ -n "$TOKEN" ] || { echo "devtoken が取れない" >&2; exit 1; }

SERVE_ENV=(FLAGS_SOURCE=file FLAGS_FILE=deploy/compose/flagd/flags.json
  PHOTO_EXTERNAL_ADDR=:8480 PHOTO_INTERNAL_ADDR=:8481 PHOTO_ADMIN_ADDR=:8482)

wait_ready() {
  for _ in $(seq 1 30); do
    curl -sf -o /dev/null http://127.0.0.1:8480/healthz && return 0
    sleep 1
  done
  return 1
}

# 処理中(コミット直前で3秒待つ)のリクエストを K 本起こし、PID の列を返す
launch_inflight() {
  reqpids=""
  for i in $(seq 1 8); do
    curl -s -o "$tmp/req$i" -w "%{http_code}" --max-time 30 \
      -X POST http://127.0.0.1:8480/v2/photos \
      -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
      -d '{"caption":"drain-drill","content_type":"image/png"}' > "$tmp/code$i" &
    reqpids="$reqpids $!"
  done
}

echo "A. serve: SIGTERM は処理中を完走させる"
start_bg "$tmp/serve.log" "${SERVE_ENV[@]}" PHOTO_FAULT=before_commit=3s "$PHOTO_BIN"
serve_pid=$last_pid
wait_ready || { ng "serve が起動しない: $(tail -3 "$tmp/serve.log")"; exit 1; }
launch_inflight
sleep 1                       # 8本全部がコミット前の3秒待ちに入っている間に…
kill -TERM "$serve_pid"       # …停止指示
for p in $reqpids; do wait "$p"; done
codes=$(cat "$tmp"/code* | tr -d '\n')
if [ "$codes" = "201201201201201201201201" ]; then
  ok "処理中の 8 リクエストが全部 201 で完走した"
else
  ng "完走しなかった: codes=$(cat "$tmp"/code* | paste -sd, -)"
fi
wait "$serve_pid" 2>/dev/null
grep -q '排水完了' "$tmp/serve.log" && ok "排水完了ログあり" || ng "排水完了ログが無い"
grep -q '"clean":true\|clean=true' "$tmp/serve.log" && ok "clean=true" || ng "clean=true でない: $(grep 排水 "$tmp/serve.log")"

echo "B. serve: SIGKILL は処理中を切断する(A が守っているものの実証)"
start_bg "$tmp/serve2.log" "${SERVE_ENV[@]}" PHOTO_FAULT=before_commit=3s "$PHOTO_BIN"
serve_pid=$last_pid
wait_ready || { ng "serve(2回目) が起動しない"; exit 1; }
launch_inflight
sleep 1
kill -9 "$serve_pid"
dropped=0
for p in $reqpids; do wait "$p" || dropped=$((dropped + 1)); done
if [ "$dropped" -gt 0 ]; then
  ok "SIGKILL では ${dropped}/8 が切断された(排水が守っている実害の実証)"
else
  ng "SIGKILL でも全部成功してしまった(ドリルの前提が崩れている)"
fi

echo "C. relay: 処理中のバッチを完走させ、残数を記録して止まる"
docker exec "$LOCALSTACK" awslocal sqs purge-queue \
  --queue-url http://localhost:4566/000000000000/gear-photo-events.fifo >/dev/null 2>&1
( cd dev && go run ./eventadmin ensure-topics >/dev/null )
for i in $(seq 1 150); do
  printf 'INSERT INTO outbox (event_id, event_type, aggregate_id, payload) VALUES ("drain%04d", "photo.drain.check", "photo:95%02d", JSON_OBJECT("id", %d));\n' "$i" $((i % 30)) "$i"
done | ${MYSQL} photo 2>/dev/null || { ng "outbox の seed に失敗"; exit 1; }
start_bg "$tmp/relay.log" RELAY_INTERVAL=1s "$PHOTO_BIN" relay
relay_pid=$last_pid
sleep 0.7                    # 1バッチ目(100件)の途中
kill -TERM "$relay_pid"
wait "$relay_pid"; relay_rc=$?
[ "$relay_rc" = 0 ] && ok "exit 0 で停止(SIGTERM を異常扱いしない)" || ng "exit=$relay_rc"
grep -q 'context canceled' "$tmp/relay.log" && ng "context canceled が出ている(バッチが中断された)" || ok "context canceled なし(バッチ完走)"
grep -q '排水して停止' "$tmp/relay.log" && ok "残数の記録あり: $(grep -o '"unsent":[0-9]*' "$tmp/relay.log" | tail -1)" || ng "排水ログが無い"
sent=$(${MYSQL} -N photo -e "SELECT COUNT(*) FROM outbox WHERE aggregate_id LIKE 'photo:95%' AND published_at IS NOT NULL;")
unsent=$(${MYSQL} -N photo -e "SELECT COUNT(*) FROM outbox WHERE aggregate_id LIKE 'photo:95%' AND published_at IS NULL;")
[ $((sent + unsent)) = 150 ] && ok "帳尻一致(送信済み $sent + 未送信 $unsent = 150)" || ng "帳尻が合わない: $sent + $unsent"

echo "D. consumer: 受信済みを処理しきってから止まり、再配信を自作しない"
start_bg "$tmp/consume1.log" "$GEAR_BIN" consume
c_pid=$last_pid
sleep 2                      # 処理の真っ最中に…
kill -TERM "$c_pid"
wait "$c_pid"; c_rc=$?
[ "$c_rc" = 0 ] && ok "exit 0 で停止" || ng "exit=$c_rc"
grep -q 'context canceled' "$tmp/consume1.log" && ng "context canceled が出ている" || ok "context canceled なし(受信済みを完走)"
grep -q '排水して停止' "$tmp/consume1.log" && ok "排水ログあり" || ng "排水ログが無い"
# 残りを全部流す(relay 再開 → 全送信、consumer 再開 → 全適用)
start_bg "$tmp/relay2.log" RELAY_INTERVAL=1s "$PHOTO_BIN" relay
relay2=$last_pid
start_bg "$tmp/consume2.log" "$GEAR_BIN" consume
c2=$last_pid
for _ in $(seq 1 60); do
  n=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM inbox WHERE event_type='photo.drain.check';")
  [ "$n" = 150 ] && break
  sleep 1
done
kill -TERM "$relay2" "$c2" 2>/dev/null; wait "$relay2" "$c2" 2>/dev/null
[ "$n" = 150 ] && ok "最終的に inbox は 150 件(取り残しゼロ)" || ng "inbox が $n 件"
skips=$(cat "$tmp"/consume*.log | grep -c '重複イベントをスキップ' || true)
[ "${skips:-0}" = 0 ] && ok "重複スキップ 0(排水が再配信を自作していない)" || ng "重複スキップが $skips 回(排水になっていない)"

echo
if [ "$fail" = 0 ]; then
  echo "すべて期待どおり"
else
  echo "期待と違う結果がある" >&2
  exit 1
fi
