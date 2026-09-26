#!/usr/bin/env bash
# api/ の OpenAPI スペックを base（既定: origin/main）と比較し、破壊的変更を検出する。
# 破壊的変更があるのにメジャーバージョン（info.version）が上がっていなければ失敗する。
#
#   dev/scripts/api-breaking.sh [base-ref]
#
# fail-closed。比較できなかった場合も失敗する（「検査できなかった」を成功と報告しない）。
set -uo pipefail
base=${1:-${API_BASE_REF:-origin/main}}
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

die() { echo "api-breaking: $*" >&2; exit 1; }

major() { python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['info']['version'].split('.')[0])" "$1"; }
major_ref() { git show "$1" 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin)['info']['version'].split('.')[0])" 2>/dev/null; }

# base ref が解決できることを先に確かめる。解決できないまま「スキップ」で成功にしない
git rev-parse --verify --quiet "$base" >/dev/null \
  || die "base ref ${base} が解決できない。fetch 済みか、API_BASE_REF が正しいか確認する"

specs=(api/*/*.openapi.json)
[ -e "${specs[0]}" ] || die "api/ にスペックが1つも無い。先に mise run api を実行する"

status=0
compared=0
new_specs=0
for spec in "${specs[@]}"; do
  if ! git cat-file -e "${base}:${spec}" 2>/dev/null; then
    echo "  ${spec}: base に存在しない（新規API）"
    new_specs=$((new_specs + 1))
    continue
  fi
  compared=$((compared + 1))
  out=$(oasdiff breaking "${base}:${spec}" "$spec" --fail-on ERR --format text --color never 2>&1)
  code=$?
  if [ $code -eq 0 ]; then
    echo "  ${spec}: 破壊的変更なし"
    continue
  fi
  echo "  ${spec}: 破壊的変更を検出"
  echo "$out" | sed 's/^/    /'
  base_major=$(major_ref "${base}:${spec}")
  rev_major=$(major "$spec")
  if [ -z "$base_major" ]; then
    echo "    → ERROR: base 側の info.version が読めない" >&2
    status=1
  elif [ "$rev_major" -gt "$base_major" ]; then
    echo "    → メジャーバージョンが ${base_major} → ${rev_major} に更新済み。/v${rev_major} の並行提供を確認すること"
  else
    echo "    → ERROR: メジャーバージョンが未更新（base=${base_major}, revision=${rev_major}）。" >&2
    echo "      破壊的変更にはメジャー更新と旧バージョンの並行提供が必要（api-design §3.4）。" >&2
    status=1
  fi
done

echo "  比較したスペック: ${compared} / 新規: ${new_specs} / 全体: ${#specs[@]}"
# 1つも比較できなかった場合は失敗させる。全スペックが新規である状態は、
# base に api/ がある通常の運用ではあり得ない（ゲートが空振りしている印）
if [ "$compared" -eq 0 ]; then
  die "base ${base} と比較できたスペックが0件。ゲートが空振りしている（base の api/ を確認する）"
fi
exit $status
