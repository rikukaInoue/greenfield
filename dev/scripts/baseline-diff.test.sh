#!/usr/bin/env bash
# baseline-diff.sh（#191 / ADR 0021）が、破るべき形をちゃんと落とすかを1件ずつ検査する。
#
#   dev/scripts/baseline-diff.test.sh
#
# 検査の検査。ゲート自身が壊れて素通しになる形（fail-open）を作らないための固定。
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

future=$(date -v +30d +%Y-%m-%d 2>/dev/null || date -d "+30 days" +%Y-%m-%d)
past="2020-01-01"

# run <期待exit> <名前> <baseline内容(空文字=ファイル無し)> <findings>
run() {
  local want=$1 name=$2 baseline=$3 findings=$4
  local file="$work/$name.baseline"
  if [ "$baseline" != "__MISSING__" ]; then
    printf '%s' "$baseline" > "$file"
  else
    rm -f "$file"
  fi
  printf '%s' "$findings" | "$here/baseline-diff.sh" "$file" "$name" > "$work/$name.out" 2>&1
  local got=$?
  if [ "$got" -eq "$want" ]; then
    ok "${name}（exit=${got}）"
  else
    ng "${name}: exit=${got}, want ${want}"
    sed 's/^/    /' "$work/$name.out" >&2
  fi
}

echo "1. 通るべき形"
run 0 "既知の検出だけなら緑" "CVE-2026-0001 photo  # 到達不能をgovulncheckで確認 exp:${future} #999
" "CVE-2026-0001 photo"
run 0 "検出もbaselineも空なら緑" "" ""
run 0 "解消済みは警告どまり" "CVE-2026-0001 photo  # 対応待ち exp:${future} #999
CVE-2026-0002 gear  # 対応待ち exp:${future} #999
" "CVE-2026-0001 photo"

echo "2. 落ちるべき形"
run 1 "新規検出は落ちる" "" "CVE-2026-0003 core"
run 1 "baselineが無いのは素通しでなく失敗" "__MISSING__" "whatever"
run 1 "理由の無い除外は受け付けない" "CVE-2026-0001 photo
" "CVE-2026-0001 photo"
run 1 "期限の無い除外は受け付けない" "CVE-2026-0001 photo  # 理由だけ書いた #999
" "CVE-2026-0001 photo"
run 1 "期限切れの除外は放置として落ちる" "CVE-2026-0001 photo  # 対応待ち exp:${past} #999
" "CVE-2026-0001 photo"
run 1 "検出0件なのにbaseline有りは空振りとして落ちる" "CVE-2026-0001 photo  # 対応待ち exp:${future} #999
" ""

echo "3. 出力の形（比較した件数が言えること）"
if grep -q "比較した対象: 検出 1 / baseline 2 / 新規 0 / 解消済み 1" "$work/解消済みは警告どまり.out"; then
  ok "件数の内訳を出力する"
else
  ng "件数の内訳が出ない"; cat "$work/解消済みは警告どまり.out" >&2
fi

[ "$fail" -eq 0 ] && echo "baseline-diff.test: 全て期待どおり" || echo "baseline-diff.test: 失敗あり" >&2
exit $fail
