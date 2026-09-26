#!/usr/bin/env bash
# api/ の OpenAPI スペックを base（既定: origin/main）と比較し、破壊的変更を検出する。
# 破壊的変更があるのにメジャーバージョン（info.version）が上がっていなければ失敗する
# （oasdiff の破壊的変更検出とパッケージのメジャーバージョンを機械的に連動させる。conventions/api-design.md §3.4）。
#
#   dev/scripts/api-breaking.sh [base-ref]
#
# 破壊的変更が必要な場合の手順: メジャーを上げ、旧バージョンは /v1 として並行提供する（2.2 で完成させる）。
set -uo pipefail
base=${1:-${API_BASE_REF:-origin/main}}
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

major() { python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['info']['version'].split('.')[0])" "$1"; }
major_ref() { git show "$1" 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin)['info']['version'].split('.')[0])" 2>/dev/null; }

status=0
found_any=0
for spec in api/*/*.openapi.json; do
  if ! git cat-file -e "$base:$spec" 2>/dev/null; then
    echo "  ${spec}: base にスペックがない（新規API）— 検査をスキップ"
    continue
  fi
  found_any=1
  out=$(oasdiff breaking "$base:$spec" "$spec" --fail-on ERR --format text --color never 2>&1)
  code=$?
  if [ $code -eq 0 ]; then
    echo "  ${spec}: 破壊的変更なし"
    continue
  fi
  echo "  ${spec}: 破壊的変更を検出"
  echo "$out" | sed 's/^/    /'
  base_major=$(major_ref "$base:$spec")
  rev_major=$(major "$spec")
  if [ -n "$base_major" ] && [ "$rev_major" -gt "$base_major" ]; then
    echo "    → メジャーバージョンが ${base_major} → ${rev_major} に更新済み。/v${rev_major} の並行提供を確認すること"
  else
    echo "    → ERROR: メジャーバージョンが未更新（base=${base_major}, revision=${rev_major}）。" >&2
    echo "      破壊的変更にはメジャー更新と旧バージョンの並行提供が必要（api-design §3.4）。" >&2
    status=1
  fi
done
if [ $found_any -eq 0 ]; then
  echo "  比較対象のスペックが base にない"
fi
exit $status
