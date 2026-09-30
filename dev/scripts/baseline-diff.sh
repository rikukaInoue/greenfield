#!/usr/bin/env bash
# 継続的検査の差分運用の共通機構（#191 / docs/adr/0021）。
#
#   <検査コマンド(1行1検出の正規化済み出力)> | dev/scripts/baseline-diff.sh <baselineファイル> <検査名>
#
# 検査は毎回同じ結果を出すので、既知の検出（baseline）を固定し**新規だけ**を失敗にする。
# そうしないと3回目から誰も見なくなり、緑でも赤でも意味の無い飾りになる。
#
# baseline の書式（1行1件）:
#   <検出キー>  # 理由 exp:YYYY-MM-DD #issue番号
#
# 規律（ADR 0021）:
#   1. 理由の無い除外は受け付けない（半年後に誰も外せなくなる）
#   2. 期限（exp:）の無い除外は受け付けない。期限切れは失敗＝放置の検出
#   3. 新規0件なら静かに緑。新規が出たときだけ落ちて目に入る
#
# fail-open にしない（監査 A-1 と同じ話）:
#   - baseline ファイルが無い → 失敗（「見つからないので全部素通し、緑」を作らない）
#   - 検出0件なのに baseline に件がある → 検査自体が空振りしている可能性が高いので失敗
#     （本当に全件解消したなら baseline を空にする。その diff がレビューの記録になる）
set -eu

BASELINE="${1:?usage: <findings> | baseline-diff.sh <baseline-file> <name>}"
NAME="${2:-baseline-diff}"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

if [ ! -f "$BASELINE" ]; then
  echo "$NAME: baseline が無い: ${BASELINE}（新規導入なら空ファイルをコミットする。無い状態での素通しは作らない）" >&2
  exit 1
fi

today=$(date +%Y-%m-%d)
known_keys=$(mktemp)
trap 'rm -f "$known_keys"' EXIT

# --- baseline の検品: 書式・理由・期限 ---
lineno=0
baseline_count=0
while IFS= read -r line || [ -n "$line" ]; do
  lineno=$((lineno + 1))
  # 空行と行頭コメントは飛ばす
  case "$line" in "" | \#*) continue ;; esac
  key="${line%%#*}"
  key="$(echo "$key" | sed 's/[[:space:]]*$//')"
  note="${line#*#}"
  if [ "$note" = "$line" ] || [ -z "$(echo "$note" | tr -d '[:space:]')" ]; then
    ng "$BASELINE:$lineno: 理由が無い（書式: <検出キー>  # 理由 exp:YYYY-MM-DD #issue）"
    continue
  fi
  exp=$(echo "$note" | grep -oE 'exp:[0-9]{4}-[0-9]{2}-[0-9]{2}' | head -1 | cut -c5-)
  if [ -z "$exp" ]; then
    ng "$BASELINE:$lineno: 期限（exp:YYYY-MM-DD）が無い。期限の無い除外は永久に残る"
    continue
  fi
  if [ "$exp" \< "$today" ]; then
    ng "$BASELINE:$lineno: 除外の期限切れ（exp:${exp}）。直すか、理由を更新して期限を延ばす: $key"
    continue
  fi
  printf '%s\n' "$key" >> "$known_keys"
  baseline_count=$((baseline_count + 1))
done < "$BASELINE"

# --- 検出との突き合わせ ---
findings=$(cat)
finding_count=0
new_count=0
if [ -n "$findings" ]; then
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    finding_count=$((finding_count + 1))
    if ! grep -qxF "$f" "$known_keys"; then
      ng "新規: $f"
      new_count=$((new_count + 1))
    fi
  done <<EOF
$findings
EOF
fi

# 解消済み（baseline にあるが今回検出されなかったもの）は掃除を促す。
# 失敗にはしない: 検出が揺れる検査（DAST 等）で偽の赤を作らないため
resolved=0
while IFS= read -r k; do
  [ -z "$k" ] && continue
  if ! printf '%s\n' "$findings" | grep -qxF "$k"; then
    echo "  解消済み?: ${k}（もう検出されない。baseline から消す）"
    resolved=$((resolved + 1))
  fi
done < "$known_keys"

echo "$NAME: 比較した対象: 検出 $finding_count / baseline $baseline_count / 新規 $new_count / 解消済み $resolved"

# 空振りの検出: 以前は baseline_count 件見つけていた検査が突然0件は、
# 直ったのではなく検査が壊れた可能性の方が高い
if [ "$finding_count" -eq 0 ] && [ "$baseline_count" -gt 0 ]; then
  echo "$NAME: 検出が0件なのに baseline に $baseline_count 件ある。検査が空振りしていないか確かめる（全件解消なら baseline を空にする）" >&2
  exit 1
fi

exit $fail
