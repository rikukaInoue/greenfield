#!/usr/bin/env bash
# 負荷をかけたままカラム改名を完走させるドリル（検証 #25 / #21 / #15）。
#
#   dev/scripts/rename-drill.sh [duration]
#
# 手順: 負荷開始 → expand（済）→ 二重書き（済）→ バックフィル → フラグ 1%→50%→100%
#        → OFF 巻き戻し → 100% → 負荷終了。エラー0・一致100% を期待する。
set -euo pipefail
duration=${1:-70s}
root=$(cd "$(dirname "$0")/.." && cd .. && pwd)
cd "$root"

flagfile=deploy/compose/flagd/flags.json
setflag() {
  python3 - "$1" <<'PY'
import json, pathlib, sys
p = pathlib.Path("deploy/compose/flagd/flags.json")
d = json.loads(p.read_text())
f = d["flags"]["release.photo_caption_to_title"]
spec = sys.argv[1]
if spec in ("on", "off"):
    f["defaultVariant"] = spec
    f.pop("targeting", None)
else:
    pct = int(spec)
    f["defaultVariant"] = "off"
    f["targeting"] = {
        "fractional": [
            {"var": "targetingKey"},
            ["on", pct],
            ["off", 100 - pct],
        ]
    }
p.write_text(json.dumps(d, indent=2, ensure_ascii=False) + "\n")
PY
  printf '  release.photo_caption_to_title -> %s\n' "$1"
  sleep 3
}

restore() { git checkout -- "$flagfile" 2>/dev/null || true; }
trap restore EXIT

echo "=== 負荷を開始（${duration}） ==="
go run ./dev/loadgen -duration "$duration" -rps 20 -quiet > /tmp/loadgen.json 2>/tmp/loadgen.err &
load_pid=$!
sleep 3

echo "=== バックフィル（負荷中） ==="
go run ./services/photo/cmd/photo backfill title --batch 50 --pause 100ms 2>&1 | grep -oE "filled=[0-9]+ batches=[0-9]+" | sed 's/^/  /' || true
go run ./dev/columncheck -old caption -new title | sed 's/^/  /'

echo "=== フラグの段階展開 ==="
for pct in 1 50 100; do
  setflag "$pct"
  go run ./dev/columncheck -old caption -new title | sed 's/^/    /'
done

echo "=== OFF 巻き戻しドリル（再デプロイなし） ==="
setflag off
echo "=== 100% へ戻す ==="
setflag on

echo "=== 負荷の終了を待つ ==="
wait $load_pid && load_ok=0 || load_ok=1
cat /tmp/loadgen.json

echo "=== 最終検算 ==="
go run ./dev/columncheck -old caption -new title | sed 's/^/  /'
errors=$(python3 -c "import json;print(json.load(open('/tmp/loadgen.json'))['errors'])")
echo
if [ "$errors" = "0" ] && [ "$load_ok" = "0" ]; then
  echo "結果: リクエストエラー0件・新旧カラム一致100% で完走"
else
  echo "結果: 失敗（errors=$errors）" >&2
  head -20 /tmp/loadgen.err >&2 || true
  exit 1
fi
