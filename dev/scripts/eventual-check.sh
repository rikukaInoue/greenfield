#!/usr/bin/env bash
# Eventual（outbox + relay + SNS/SQS + inbox。ステージ 4.2）の一気通貫を確かめる。
#
#   dev/scripts/eventual-check.sh
#
# 前提: mysql(13306) / localstack(4566) が起動済みで、photo / gear のマイグレーションが
#       適用済み、トピックとキューが用意済み（mise run events:up）。
#       photo・gear のバイナリは PHOTO_BIN / GEAR_BIN で渡す（既定は go run）。
#
# 確かめること（check #8 / #9）:
#   - relay 停止中の公開は outbox に未送信として残る（業務データと同一 tx）
#   - relay を起動すると遅れて必ず届く（回復。#8）
#   - 同一イベントを再配送しても副作用は1回きり（inbox が弾く。#9）
#   - Publish は Atomic.Do の外では呼べない（ADR 0012。単体テストが担保、ここでは再掲しない）
set -eu

MYSQL="${MYSQL_CMD:-mysql -h 127.0.0.1 -P 13306 -uroot -proot}"
PHOTO_BIN="${PHOTO_BIN:-}"
GEAR_BIN="${GEAR_BIN:-}"
QUEUE_URL="${GEAR_EVENT_QUEUE_URL:-http://localhost:4566/000000000000/gear-photo-events.fifo}"
LOCALSTACK="${LOCALSTACK_CONTAINER:-greenfield-localstack}"

# AWS_ENDPOINT_URL=aws で**実 AWS**(7.3 の再演)。SDK の既定解決に任せ、資格情報も環境のものを使う
REAL=0
if [ "${AWS_ENDPOINT_URL:-}" = "aws" ]; then
  REAL=1
  unset AWS_ENDPOINT_URL
  export AWS_REGION="${AWS_REGION:-ap-northeast-1}"
else
  export AWS_ENDPOINT_URL="${AWS_ENDPOINT_URL:-http://localhost:4566}"
  export AWS_REGION="${AWS_REGION:-us-east-1}"
  export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
  export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-testtest}"
fi
export ENV="${ENV:-dev}"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

tmp=$(mktemp -d)
pids=""
cleanup() {
  for p in ${pids}; do kill "$p" 2>/dev/null || true; done
  rm -rf "$tmp"
}
trap cleanup EXIT

# go run を常駐で使うと kill が go run にしか届かず実バイナリが孤児になる
# （ポーリングを続けて後続の検査のメッセージを黙って横取りする。ローカルで実際に踏んだ）。
# 先にビルドし、常駐は exec 付きサブシェルで起動して kill が本体に届くようにする。
if [ -z "$PHOTO_BIN" ]; then
  PHOTO_BIN="$tmp/photo"; ( cd services/photo && GOWORK=off go build -o "$PHOTO_BIN" ./cmd/photo )
fi
if [ -z "$GEAR_BIN" ]; then
  GEAR_BIN="$tmp/gear"; ( cd services/gear && GOWORK=off go build -o "$GEAR_BIN" ./cmd/gear )
fi
photo() { "$PHOTO_BIN" "$@"; }
gear()  { "$GEAR_BIN" "$@"; }
start_bg() { # start_bg <logfile> <env...> <cmd...> — exec で子を作らず起動（kill が届く）
  local log=$1; shift
  ( exec env "$@" > "$log" 2>&1 ) &
  pids="$pids $!"
}

# 検証のたびに前回の痕跡で結果が変わらないよう、専用の印で作った行だけ消す
${MYSQL} photo -e "DELETE FROM outbox WHERE aggregate_id LIKE 'photo:90%';" >/dev/null 2>&1
${MYSQL} gear  -e "DELETE FROM inbox WHERE event_type = 'photo.published.check';" >/dev/null 2>&1

EVENT_ID=$(python3 -c 'import secrets;print(secrets.token_hex(16))')
AGG="photo:9001"

echo "1. relay 停止中: 業務データと同一 tx で outbox に残る（#8 前半）"
# usecase を通した公開は E2E（認証・ストレージ込み）でしか作れないため、ここでは
# 「relay が未送信をどう扱うか」に絞って outbox へ直接1行置く。
# **同一 tx であること自体は単体テスト（ADR 0012 のガード）と photo の Publish テストが担保する**
${MYSQL} photo <<SQL
INSERT INTO outbox (event_id, event_type, aggregate_id, payload)
VALUES ('${EVENT_ID}', 'photo.published.check', '${AGG}', JSON_OBJECT('id', 9001));
SQL
N=$(${MYSQL} -N photo -e "SELECT COUNT(*) FROM outbox WHERE event_id='${EVENT_ID}' AND published_at IS NULL;" 2>/dev/null)
[ "$N" = 1 ] && ok "未送信として outbox にある" || ng "outbox に無い: $N"

echo
echo "2. relay を起動すると遅れて届く（#8 後半）"
start_bg "$tmp/relay.log" RELAY_INTERVAL=1s "$PHOTO_BIN" relay
for i in $(seq 1 30); do
  P=$(${MYSQL} -N photo -e "SELECT published_at IS NOT NULL FROM outbox WHERE event_id='${EVENT_ID}';" 2>/dev/null)
  [ "$P" = 1 ] && break
  sleep 1
done
[ "${P:-0}" = 1 ] && ok "relay 起動後に送信済みになった" || ng "送信されない: $(tail -3 "$tmp/relay.log")"

echo
echo "3. 受信側: inbox に記録され業務処理が1回だけ走る"
start_bg "$tmp/consume.log" "$GEAR_BIN" consume
for i in $(seq 1 30); do
  N=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM inbox WHERE event_id='${EVENT_ID}';" 2>/dev/null)
  [ "$N" = 1 ] && break
  sleep 1
done
[ "${N:-0}" = 1 ] && ok "inbox に記録された（バス経由で届いた）" || ng "届かない: $(tail -5 "$tmp/consume.log")"

echo
echo "4. 重複配送の無害化（#9）"
# SQS の 5分窓の重複排除を迂回するため、MessageDeduplicationId だけ変えて同じイベントを送る。
# **バスの重複排除ではなく inbox が弾いていることを見るための手順**
BODY=$(python3 -c "import json;print(json.dumps({'id':'${EVENT_ID}','type':'photo.published.check','aggregate_id':'${AGG}','payload':{'id':9001}}))")
if [ "$REAL" = 1 ]; then
  aws sqs send-message \
    --queue-url "$QUEUE_URL" --message-body "$BODY" \
    --message-group-id "$AGG" --message-deduplication-id "dup-${EVENT_ID}" >/dev/null
else
  docker exec "$LOCALSTACK" awslocal sqs send-message \
    --queue-url "$QUEUE_URL" --message-body "$BODY" \
    --message-group-id "$AGG" --message-deduplication-id "dup-${EVENT_ID}" >/dev/null
fi
for i in $(seq 1 20); do
  grep -q '重複イベントをスキップ' "$tmp/consume.log" && break
  sleep 1
done
grep -q '重複イベントをスキップ' "$tmp/consume.log" && ok "2回目はスキップされた" || ng "重複がスキップされない: $(tail -5 "$tmp/consume.log")"
N=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM inbox WHERE event_id='${EVENT_ID}';" 2>/dev/null)
[ "$N" = 1 ] && ok "inbox は1行のまま（副作用1回）" || ng "inbox が $N 行"

echo
echo "5. outbox からの再送（再生の正。#10 の下地）"
SINCE=$(python3 -c "import datetime;print((datetime.datetime.now(datetime.UTC)-datetime.timedelta(hours=1)).strftime('%Y-%m-%dT%H:%M:%SZ'))")
photo republish --since "$SINCE" > "$tmp/republish.log" 2>&1 \
  && ok "送信済みイベントを再送できた（$(grep -o '"count":[0-9]*' "$tmp/republish.log" | head -1)）" \
  || ng "再送に失敗: $(tail -3 "$tmp/republish.log")"
# 再送も at-least-once の一形態。inbox が吸収して副作用は増えない
sleep 5
N=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM inbox WHERE event_id='${EVENT_ID}';" 2>/dev/null)
[ "$N" = 1 ] && ok "再送しても inbox は1行のまま" || ng "再送で inbox が $N 行"

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; fi
exit "$fail"
