#!/usr/bin/env bash
# 負荷をかけたままカラム改名を完走させるドリル（検証 #25 / #21 / #15）。
#
#   dev/scripts/rename-drill.sh --old caption --new title \
#       --flag release.photo_caption_to_title \
#       --backfill "go run ./services/photo/cmd/photo backfill title --batch 50 --pause 100ms" \
#       [--duration 70s]
#
# 手順: 負荷開始 → バックフィル → フラグ 1%→50%→100% → OFF 巻き戻し → 100% → 負荷終了。
# 期待: リクエストエラー0件・新旧カラム一致100%。
#
# fail-closed。前提（フラグ定義・両カラムの存在・バックフィルコマンド）が欠けていれば
# 何も実行せずに失敗する。空振りしたまま成功と報告しない。
set -uo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

die() { echo "rename-drill: $*" >&2; exit 1; }

old="" new="" flag="" backfill="" duration=70s
while [ $# -gt 0 ]; do
  case "$1" in
    --old) old=${2:?}; shift 2 ;;
    --new) new=${2:?}; shift 2 ;;
    --flag) flag=${2:?}; shift 2 ;;
    --backfill) backfill=${2:?}; shift 2 ;;
    --duration) duration=${2:?}; shift 2 ;;
    *) die "不明な引数: $1" ;;
  esac
done
[ -n "$old" ] && [ -n "$new" ] || die "--old と --new は必須"
[ -n "$flag" ] || die "--flag は必須（読み切替に使う release フラグ名）"
[ -n "$backfill" ] || die "--backfill は必須（バックフィルを流すコマンド。改名ごとに異なる）"

# ツールは先にビルドする。go run は終了コードを伝播せず常に 1 を返すため、
# columncheck の「カラムが無い（2）」と「一致しない（1）」を区別できない。
# 負荷中にコンパイルが走るのを避ける意味もある。
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
go build -o "$bin/columncheck" ./dev/columncheck || die "columncheck のビルドに失敗した"
go build -o "$bin/loadgen" ./dev/loadgen || die "loadgen のビルドに失敗した"

flagfile=deploy/compose/flagd/flags.json
[ -f "$flagfile" ] || die "$flagfile が無い"
python3 -c "
import json, sys
d = json.load(open('$flagfile'))
if '$flag' not in d['flags']:
    sys.exit('フラグ $flag が $flagfile に無い。expand とフラグ定義を先に入れる')
" || exit 1

# 負荷を流す前に両カラムの存在を確かめる（expand 前や contract 後には実行しない）。
# columncheck は 2 を返してどのカラムが無いかを出す
exists_out=$("$bin/columncheck" -old "$old" -new "$new" -exists-only 2>&1)
case $? in
  0) echo "  前提: $exists_out" ;;
  2) die "$exists_out — expand 済みか、contract 前かを確認する" ;;
  *) die "カラムの確認に失敗した: $exists_out" ;;
esac

setflag() {
  python3 - "$1" <<'PY'
import json, os, pathlib, sys
p = pathlib.Path(os.environ["FLAGFILE"])
d = json.loads(p.read_text())
f = d["flags"][os.environ["FLAG"]]
spec = sys.argv[1]
if spec in ("on", "off"):
    f["defaultVariant"] = spec
    f.pop("targeting", None)
else:
    pct = int(spec)
    f["defaultVariant"] = "off"
    f["targeting"] = {"fractional": [{"var": "targetingKey"}, ["on", pct], ["off", 100 - pct]]}
p.write_text(json.dumps(d, indent=2, ensure_ascii=False) + "\n")
PY
  [ $? -eq 0 ] || die "フラグの書き換えに失敗した"
  printf '  %s -> %s\n' "$FLAG" "$1"
  sleep 3
}
export FLAGFILE="$flagfile" FLAG="$flag"

cleanup() {
  git checkout -- "$flagfile" 2>/dev/null || true
  rm -rf "$bin"
}
trap cleanup EXIT

check() { "$bin/columncheck" -old "$old" -new "$new"; }

echo "=== 負荷を開始（${duration}） ==="
"$bin/loadgen" -duration "$duration" -rps 20 -quiet > /tmp/loadgen.json 2>/tmp/loadgen.err &
load_pid=$!
sleep 3

echo "=== バックフィル（負荷中） ==="
# 失敗を飲まない。ここが空振りするとドリル全体が無意味になる
eval "$backfill" 2>&1 | sed 's/^/  /' || die "バックフィルが失敗した: $backfill"
check | sed 's/^/  /' || die "バックフィル後に一致しなかった"

echo "=== フラグの段階展開 ==="
for pct in 1 50 100; do
  # 前置きゲート(#200): 一致していない状態では**次の%へ進めない**。
  # 後置きの検査だけだと「進めてよいか」は人間の判断のままになる
  check > /dev/null || die "一致していないので ${pct}% への展開をブロックした"
  setflag "$pct"
  check | sed 's/^/    /' || die "フラグ ${pct}% の時点で一致しなかった"
done

echo "=== OFF 巻き戻しドリル（再デプロイなし） ==="
setflag off
echo "=== 100% へ戻す ==="
setflag on

echo "=== 負荷の終了を待つ ==="
wait $load_pid && load_ok=0 || load_ok=1
cat /tmp/loadgen.json

echo "=== 最終検算 ==="
check | sed 's/^/  /' || die "最終検算で一致しなかった"
errors=$(python3 -c "import json;print(json.load(open('/tmp/loadgen.json'))['errors'])") || die "負荷の結果が読めない"
requests=$(python3 -c "import json;print(json.load(open('/tmp/loadgen.json'))['requests'])")
[ "$requests" -gt 0 ] || die "リクエストが1件も流れていない（負荷が空振りしている）"

echo
if [ "$errors" = "0" ] && [ "$load_ok" = "0" ]; then
  echo "結果: ${requests} リクエスト・エラー0件・新旧カラム一致100% で完走"
else
  echo "結果: 失敗（requests=${requests} errors=${errors}）" >&2
  head -20 /tmp/loadgen.err >&2 || true
  exit 1
fi
