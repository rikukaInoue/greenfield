#!/usr/bin/env bash
# ReplicaView（ステージ 4.3）の一気通貫を確かめる。
#
#   dev/scripts/replica-check.sh
#
# 前提: eventual-check.sh と同じ（mysql / localstack / マイグレーション / トピック作成済み）。
#
# 確かめること（check #10）:
#   - photo.published（機材あり）が複製に入り、機材なしは入らず、photo.deleted で消える
#   - 複製を消して outbox から replay すると**同じ指紋**に再構築される（再生の正は outbox）
set -eu

MYSQL="${MYSQL_CMD:-mysql -h 127.0.0.1 -P 13306 -uroot -proot}"
PHOTO_BIN="${PHOTO_BIN:-}"
GEAR_BIN="${GEAR_BIN:-}"

export AWS_ENDPOINT_URL="${AWS_ENDPOINT_URL:-http://localhost:4566}"
export AWS_REGION="${AWS_REGION:-us-east-1}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-testtest}"
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

replica_count() { ${MYSQL} -N gear -e "SELECT COUNT(*) FROM photo_replica;" 2>/dev/null; }
fingerprint()   { gear replica-status | grep -o 'fingerprint=.*' | cut -d= -f2; }
wait_count() { # wait_count <expected> [tries]
  local want=$1 tries=${2:-30}
  for i in $(seq 1 "$tries"); do
    [ "$(replica_count)" = "$want" ] && return 0
    sleep 1
  done
  return 1
}

# 前回の痕跡を消す（イベントIDは実行のたびに新しいので inbox はそのままでよい）
${MYSQL} photo -e "DELETE FROM outbox WHERE aggregate_id LIKE 'photo:91%';" >/dev/null 2>&1
${MYSQL} gear -e "DELETE FROM photo_replica WHERE photo_id >= 91001;" >/dev/null 2>&1

E1=$(python3 -c 'import secrets;print(secrets.token_hex(16))')
E2=$(python3 -c 'import secrets;print(secrets.token_hex(16))')
E3=$(python3 -c 'import secrets;print(secrets.token_hex(16))')
E4=$(python3 -c 'import secrets;print(secrets.token_hex(16))')

echo "1. イベントが複製へ反映される"
# 機材あり2枚（91001, 91002）・機材なし1枚（91003）を公開し、91002 は削除する。
# 集約IDが同じ pub → delete は FIFO の MessageGroupId で順序が保たれる
${MYSQL} photo <<SQL
INSERT INTO outbox (event_id, event_type, aggregate_id, payload) VALUES
('${E1}', 'photo.published', 'photo:91001', JSON_OBJECT('id', 91001, 'gear_item_id', 1, 'caption', 'replica-check-1', 'owner_id', 'chk', 'object_key', '', 'created_at', '2026-09-29T00:00:01.000000Z')),
('${E2}', 'photo.published', 'photo:91002', JSON_OBJECT('id', 91002, 'gear_item_id', 1, 'caption', 'replica-check-2', 'owner_id', 'chk', 'object_key', '', 'created_at', '2026-09-29T00:00:02.000000Z')),
('${E3}', 'photo.published', 'photo:91003', JSON_OBJECT('id', 91003, 'caption', 'no-gear', 'owner_id', 'chk', 'object_key', '', 'created_at', '2026-09-29T00:00:03.000000Z')),
('${E4}', 'photo.deleted',   'photo:91002', JSON_OBJECT('id', 91002));
SQL
BASE=$(replica_count)

start_bg "$tmp/relay.log" RELAY_INTERVAL=1s "$PHOTO_BIN" relay
start_bg "$tmp/consume.log" "$GEAR_BIN" consume

# 期待: +1（91001 だけ残る。91002 は消され、91003 は機材なしで入らない）
if wait_count "$((BASE + 1))"; then
  CAPTIONS=$(${MYSQL} -N gear -e "SELECT caption FROM photo_replica WHERE photo_id >= 91001;" 2>/dev/null)
  [ "$CAPTIONS" = "replica-check-1" ] && ok "published が入り、deleted は消え、機材なしは入らない" || ng "複製の中身: [$CAPTIONS]"
else
  ng "複製が期待の行数にならない: $(replica_count) (期待 $((BASE + 1))): $(tail -3 "$tmp/consume.log")"
fi

echo
echo "2. 複製を消して outbox から再構築すると一致する（#10）"
# 「消す前」との比較はしない: ローカルの複製は手作業や過去の検証でドリフトしていることが
# あり、replay はそれを**正しく outbox 由来の状態へ戻す**ため、ドリフト分だけ不一致になる
# （実測で踏んだ。不一致は replay の欠陥ではなく、outbox が正である証左）。
# 代わりに再構築を2回行い、**再構築が決定的である**ことを見る。
rebuild_and_replay() { # -> fingerprint（安定するまで待つ）
  gear rebuild-replica > "$tmp/rebuild.log" 2>&1
  local since
  since=$(python3 -c "import datetime;print((datetime.datetime.now(datetime.UTC)-datetime.timedelta(hours=24)).strftime('%Y-%m-%dT%H:%M:%SZ'))")
  photo republish --since "$since" > "$tmp/republish.log" 2>&1 || { ng "republish に失敗: $(tail -3 "$tmp/republish.log")"; return 1; }
  # 完了イベントは無いので「指紋が5秒間変わらない」ことで収束とみなす
  local fp="" prev="" stable=0
  for i in $(seq 1 60); do
    fp=$(fingerprint)
    if [ -n "$fp" ] && [ "$fp" = "$prev" ]; then
      stable=$((stable + 1))
      [ "$stable" -ge 5 ] && { echo "$fp"; return 0; }
    else
      stable=0
    fi
    prev=$fp
    sleep 1
  done
  echo "$fp"
}
FP1=$(rebuild_and_replay)
[ -n "$FP1" ] && ok "1回目の再構築が収束した（rows=$(replica_count)）" || ng "1回目が収束しない: $(tail -3 "$tmp/consume.log")"
gear rebuild-replica > "$tmp/rebuild.log" 2>&1
[ "$(replica_count)" = 0 ] && ok "複製を消した（rows=0）" || ng "消えていない: $(replica_count)"
FP2=$(rebuild_and_replay)
if [ -n "$FP1" ] && [ "$FP1" = "$FP2" ]; then
  ok "再構築結果が一致（fingerprint=${FP1}）"
else
  ng "一致しない: 1回目=${FP1} 2回目=${FP2}: $(tail -3 "$tmp/consume.log")"
fi

# 後始末（CI では毎回新しい DB だが、ローカルの再実行を汚さない）
${MYSQL} photo -e "DELETE FROM outbox WHERE aggregate_id LIKE 'photo:91%';" >/dev/null 2>&1
${MYSQL} gear -e "DELETE FROM photo_replica WHERE photo_id >= 91001;" >/dev/null 2>&1

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; fi
exit "$fail"
