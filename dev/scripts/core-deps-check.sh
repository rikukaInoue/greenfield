#!/usr/bin/env bash
# core の direct require を許可リスト(core/deps.allow)と突き合わせる(#270)。
#
# core の依存は core を使う全モジュールに配られる。ADR 0019 は「core に direct require を
# 足す PR には対応する ADR 番号を書く」と定めたが、文章だけの運用だったため、#162 で
# AWS SDK(SNS / SQS)が ADR も表の更新も無いまま core に入った。core/eventbus を使うのは
# photo / gear だけなのに、platform/authz まで AWS SDK を依存グラフに抱えていた
# (#270 で eventbus/snssqs へ移した)。文章だけの禁止は 1 行の違反が検知されないので、機械で止める。
#
# 検査は3点:
#   1. core/go.mod の direct require はすべて許可リストにある
#   2. 許可リストに、もう direct require でないモジュールが残っていない(リストを実物に合わせる)
#   3. test 区分のモジュールは _test.go からしか import されていない
#      (GOWORK=off go mod why -m の経路に .test が挟まる。ADR 0019 の判別法)
#
# 前提の検証(fail open にしない): direct require と許可リストの件数を出力し、0 件なら失敗させる。
set -euo pipefail
cd "$(dirname "$0")/../.."

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }

# go.mod の require ブロック/単行から、// indirect でないものを拾う
direct=$(awk '
  /^require \(/ { inblock=1; next }
  inblock && /^\)/ { inblock=0; next }
  inblock && !/\/\/ indirect/ && NF >= 2 { print $1 }
  /^require [^(]/ && !/\/\/ indirect/ { print $2 }
' core/go.mod | sort -u)
allow=$(grep -vE '^\s*(#|$)' core/deps.allow | awk '{print $1}' | sort -u)
n_direct=$(echo "$direct" | grep -c . || true)
n_allow=$(echo "$allow" | grep -c . || true)
echo "core の direct require ${n_direct}件 / 許可リスト ${n_allow}件"
if [ "$n_direct" -eq 0 ] || [ "$n_allow" -eq 0 ]; then
  echo "NG: 件数が 0。検査の前提が崩れている(go.mod か deps.allow の読み取りに失敗)" >&2
  exit 1
fi

# 1. 許可リストに無い direct require
for m in $direct; do
  if ! grep -qxF "$m" <<<"$allow"; then
    ng "core/go.mod の $m が許可リストに無い — core の全利用者に配る判断になるので、ADR を書いて core/deps.allow に足す(差し替えうる実装なら別モジュールに置く。ADR 0022)"
  fi
done
# 2. もう使っていない許可
for m in $allow; do
  if ! grep -qxF "$m" <<<"$direct"; then
    ng "core/deps.allow の $m は core/go.mod の direct require に無い — 許可リストを実物に合わせる"
  fi
done
# 3. test 区分の実態
for m in $(grep -vE '^\s*(#|$)' core/deps.allow | awk '$2 == "test" {print $1}'); do
  why=$(cd core && GOWORK=off go mod why -m "$m" 2>&1)
  if ! grep -q '\.test$' <<<"$why"; then
    ng "$m は test 区分だが、本体のコードから import されている — 全利用者に配る判断(runtime)になる:"
    echo "$why" | sed 's/^/      /' >&2
  fi
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "OK: core の direct require は許可リストどおり"
