#!/usr/bin/env bash
# api/ の OpenAPI スペックを base（既定: origin/main）と比較し、API のバージョニング規則（docs/adr/0017）を検査する。
#
#   dev/scripts/api-breaking.sh [base-ref]
#
# 規則:
#   - ファイル名とメジャーを結ぶ。<listener>.openapi.json はメジャー1（パスに接頭辞なし）、
#     <listener>.v<n>.openapi.json はメジャー n（全パスが /v<n>/ 配下）。info.version のメジャーと一致させる
#   - 既存ファイルへの破壊的変更は、メジャーを上げても通さない。次のメジャーのファイルとして並行提供する
#   - 新しいメジャーのファイルは、1つ前のメジャーが残っているときだけ足せる
#   - ファイルの削除（旧版の廃止）は、base で全操作が deprecated のときだけ許す
#
# fail-closed。比較できなかった場合も失敗する（「検査できなかった」を成功と報告しない）。
set -uo pipefail
base=${1:-${API_BASE_REF:-origin/main}}
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

die() { echo "api-breaking: $*" >&2; exit 1; }
err() { echo "    → ERROR: $*" >&2; status=1; }

git rev-parse --verify --quiet "$base" >/dev/null \
  || die "base ref ${base} が解決できない。fetch 済みか、API_BASE_REF が正しいか確認する"

# 比較器が動かないまま「破壊的変更あり / なし」を判定しない
oasdiff --version >/dev/null 2>&1 || die "oasdiff が実行できない（mise install を確認する）"

specs=(api/*/*.openapi.json)
[ -e "${specs[0]}" ] || die "api/ にスペックが1つも無い。先に mise run api を実行する"

# file_major はファイル名から期待するメジャーを返す。規則に合わない名前は空。
file_major() {
  local name
  name=$(basename "$1")
  if [[ $name =~ ^[a-z]+\.openapi\.json$ ]]; then echo 1
  elif [[ $name =~ ^[a-z]+\.v([2-9]|[1-9][0-9]+)\.openapi\.json$ ]]; then echo "${BASH_REMATCH[1]}"
  fi
}

# prev_file はメジャー n の1つ前のファイル名を返す。
prev_file() {
  local dir listener n=$2
  dir=$(dirname "$1")
  listener=$(basename "$1" | cut -d. -f1)
  if [ "$n" -eq 2 ]; then echo "$dir/$listener.openapi.json"; else echo "$dir/$listener.v$((n - 1)).openapi.json"; fi
}

# inspect は標準入力のスペックについて「メジャー / 接頭辞違反のパス数 / 非 deprecated の操作数」を返す。
inspect() {
  python3 -c '
import json, re, sys
n = int(sys.argv[1])
d = json.load(sys.stdin)
major = d["info"]["version"].split(".")[0]
bad = 0
active = 0
for path, ops in d.get("paths", {}).items():
    versioned = re.match(r"^/v[0-9]+/", path)
    if (n == 1 and versioned) or (n > 1 and not path.startswith(f"/v{n}/")):
        bad += 1
    for op in ops.values():
        if isinstance(op, dict) and not op.get("deprecated", False):
            active += 1
print(major, bad, active)
' "$1"
}

status=0
compared=0
new_specs=0
for spec in "${specs[@]}"; do
  n=$(file_major "$spec")
  if [ -z "$n" ]; then
    echo "  ${spec}: 名前が規則に合わない"
    err "<listener>.openapi.json か <listener>.v<n>.openapi.json（n≥2）にする"
    continue
  fi
  read -r major bad _ < <(inspect "$n" < "$spec")
  if [ "$major" != "$n" ]; then
    echo "  ${spec}: info.version のメジャー ${major} がファイル名のメジャー ${n} と食い違う"
    err "破壊的変更はメジャーを上げて同じファイルを書き換えるのではなく、次のメジャーのファイルとして並行提供する"
  fi
  if [ "$bad" -gt 0 ]; then
    echo "  ${spec}: パスの接頭辞が規則に合わない（${bad}件）"
    err "メジャー1は接頭辞なし、メジャー n は /v<n>/ 配下（httpapi.API.AddMajor が付ける）"
  fi

  if ! git cat-file -e "${base}:${spec}" 2>/dev/null; then
    new_specs=$((new_specs + 1))
    if [ "$n" -ge 2 ]; then
      prev=$(prev_file "$spec" "$n")
      if [ -e "$prev" ]; then
        echo "  ${spec}: 新しいメジャー ${n}（${prev} と並行提供）"
      else
        echo "  ${spec}: 新しいメジャー ${n} だが、1つ前の ${prev} が無い"
        err "新しいメジャーは旧版と並行提供する。旧版を先に消さない"
      fi
    else
      echo "  ${spec}: base に存在しない（新規API）"
    fi
    continue
  fi

  compared=$((compared + 1))
  out=$(oasdiff breaking "${base}:${spec}" "$spec" --fail-on ERR --format text --color never 2>&1)
  if [ $? -eq 0 ]; then
    echo "  ${spec}: 破壊的変更なし"
    continue
  fi
  echo "  ${spec}: 破壊的変更を検出"
  echo "$out" | sed 's/^/    /'
  err "既存のメジャーへの破壊的変更は通さない（メジャーを上げても不可）。$(basename "$spec") は残し、次のメジャー v$((n + 1)) のアダプタとして足す（httpapi.API.AddMajor）"
done

# 削除されたスペック（旧版の廃止）
while IFS= read -r removed; do
  [ -n "$removed" ] || continue
  [ -e "$removed" ] && continue
  read -r _ _ active < <(git show "${base}:${removed}" | inspect "$(file_major "$removed" || echo 1)")
  if [ "$active" -eq 0 ]; then
    echo "  ${removed}: 削除（base で全操作が deprecated。廃止予告済み）"
  else
    echo "  ${removed}: 削除されたが、base で deprecated でない操作が ${active} 件ある"
    err "旧版は、全操作を deprecated にしたリリースの後に消す"
  fi
done < <(git ls-tree -r --name-only "$base" -- api | grep -E '\.openapi\.json$')

echo "  比較したスペック: ${compared} / 新規: ${new_specs} / 全体: ${#specs[@]}"
# 1つも比較できなかった場合は失敗させる。全スペックが新規である状態は、
# base に api/ がある通常の運用ではあり得ない（ゲートが空振りしている印）
if [ "$compared" -eq 0 ]; then
  die "base ${base} と比較できたスペックが0件。ゲートが空振りしている（base の api/ を確認する）"
fi
exit $status
