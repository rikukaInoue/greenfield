#!/usr/bin/env bash
# タイムアウトヒエラルキーのドリル(10.5 / #215)。
#
# 確かめること: 依存先(gear)が**遅い**とき、photo の劣化が有界であること。
#   - 投稿は成功し続ける(紐付けは pending に留まり、回収ジョブに引き継がれる)
#   - 応答時間はクライアントタイムアウト(10s)で頭打ちになり、遅い依存先の
#     応答時間(20s)を**引き継がない**
#
# gear の代わりに「20秒黙ってから応える」スタブを立て、photo だけを起動して測る。
# 前提: mysql(13306) 起動済み、photo マイグレーション適用済み。
set -uo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export ENV=dev

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

tmp=$(mktemp -d)
pids=""
cleanup() { for p in ${pids}; do kill -9 "$p" 2>/dev/null || true; done; rm -rf "$tmp"; }
trap cleanup EXIT

PHOTO_BIN="$tmp/photo"; ( cd services/photo && GOWORK=off go build -o "$PHOTO_BIN" ./cmd/photo ) || exit 1
start_bg() { local log=$1; shift; ( exec env "$@" > "$log" 2>&1 ) & pids="$pids $!"; last_pid=$!; }

# 遅い gear スタブ: どのリクエストにも 20 秒黙ってから 200 を返す
python3 - <<'PY' > "$tmp/slowgear.log" 2>&1 &
import http.server, time
class H(http.server.BaseHTTPRequestHandler):
    def _slow(self):
        # デッドライン伝播(#268)の観測: 受け取った残り時間を記録する
        print("X-Request-Timeout-Ms=" + str(self.headers.get("X-Request-Timeout-Ms")), flush=True)
        time.sleep(20)
        self.send_response(200); self.send_header("Content-Type","application/json")
        self.end_headers(); self.wfile.write(b"{}")
    do_GET = do_POST = do_PUT = _slow
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("127.0.0.1", 8591), H).serve_forever()  # 並行: 前の呼び出しが眠っていても次のヘッダを記録する
PY
pids="$pids $!"

TOKEN=$( (cd dev && go run ./devtoken --user alice) 2>/dev/null | tail -1)
[ -n "$TOKEN" ] || { echo "devtoken が取れない" >&2; exit 1; }

start_bg "$tmp/photo.log" \
  FLAGS_SOURCE=file FLAGS_FILE=deploy/compose/flagd/flags.json \
  GEAR_INTERNAL_URL=http://127.0.0.1:8591 \
  PHOTO_EXTERNAL_ADDR=:8490 PHOTO_INTERNAL_ADDR=:8491 PHOTO_ADMIN_ADDR=:8492 \
  "$PHOTO_BIN"
photo_pid=$last_pid
for _ in $(seq 1 30); do curl -sf -o /dev/null http://127.0.0.1:8490/healthz && break; sleep 1; done

echo "1. 依存先が 20 秒黙るとき、投稿の応答は有界か"
t0=$(python3 -c 'import time;print(time.time())')
code=$(curl -s -o "$tmp/post.json" -w "%{http_code}" --max-time 25 \
  -X POST http://127.0.0.1:8490/v2/photos \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"caption":"timeout-drill","content_type":"image/png","gear_item_id":1}')
elapsed=$(python3 -c "import time;print(round(time.time()-$t0, 1))")
[ "$code" = 201 ] && ok "投稿は成功する(201)。依存先の遅さで投稿を落とさない" || ng "投稿が $code: $(cat "$tmp/post.json")"
# クライアントタイムアウト(10s)+α で頭打ちのはず。20 秒を引き継いだら失敗
python3 -c "exit(0 if $elapsed < 15 else 1)" \
  && ok "応答 ${elapsed}s(クライアントタイムアウト 10s で頭打ち。20s を引き継がない)" \
  || ng "応答 ${elapsed}s(依存先の遅さを引き継いでいる)"

echo "2. 紐付けは pending に留まる(劣化であって欠損ではない)"
status=$(python3 -c "import json;print(json.load(open('$tmp/post.json')).get('gear_link_status'))" 2>/dev/null)
if [ "$status" = "pending" ]; then
  ok "gear_link_status=pending(回収ジョブが引き継げる形で残る)"
else
  # 応答に無ければ DB を見る
  n=$(mysql -h127.0.0.1 -P13306 -uroot -proot -N photo \
    -e "SELECT COUNT(*) FROM photos WHERE caption='timeout-drill' AND gear_link_status='pending';" 2>/dev/null)
  [ "${n:-0}" -ge 1 ] && ok "DB 上で pending に留まっている" || ng "pending が残っていない(status=$status)"
fi
echo "3. デッドライン伝播(#268): 残り 3 秒で届いた投稿は、遅い下流を約 3 秒で見切る"
t0=$(python3 -c 'import time;print(time.time())')
code=$(curl -s -o "$tmp/post3.json" -w "%{http_code}" --max-time 25 \
  -X POST http://127.0.0.1:8490/v2/photos \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -H "X-Request-Timeout-Ms: 3000" \
  -d '{"caption":"timeout-drill","content_type":"image/png","gear_item_id":1}')
elapsed=$(python3 -c "import time;print(round(time.time()-$t0, 1))")
python3 -c "exit(0 if $elapsed < 5 else 1)" \
  && ok "応答 ${elapsed}s(クライアントの 10s でなく、呼び出し元の残り 3s で見切った)" \
  || ng "応答 ${elapsed}s(残り時間が下流へ伝わっていない)"
got=$(grep 'X-Request-Timeout-Ms=' "$tmp/slowgear.log" | tail -1 | cut -d= -f2)
python3 -c "exit(0 if 0 < int('${got:-0}') <= 3000 else 1)" 2>/dev/null \
  && ok "下流が受け取った残り時間 ${got}ms(3000 以下 = 伝播している)" \
  || ng "下流のヘッダ: '${got}'"
echo "   (1. の無ヘッダ投稿で下流が受け取った値: $(grep 'X-Request-Timeout-Ms=' "$tmp/slowgear.log" | head -1 | cut -d= -f2)ms = クライアント自身の上限)"

mysql -h127.0.0.1 -P13306 -uroot -proot photo -e "DELETE FROM photos WHERE caption='timeout-drill';" 2>/dev/null

kill "$photo_pid" 2>/dev/null

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; exit 1; fi
