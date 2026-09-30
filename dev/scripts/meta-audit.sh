#!/usr/bin/env bash
# 検査の検査(#226)。「ずっと成功し続けている検査」を定期的に疑うための一括実行。
#
# ここで走るのは**検査自身の回帰テスト**(検査対象のテストではない):
#   - affected の判定ツールのテスト(ADR 0015)
#   - api-breaking / baseline-diff のセルフテスト
#   - アラートルールの単体テスト(promtool)
#   - 昇格ゲート判定ロジックの単体テスト(infra/hook)
#   - querylint 自身のテスト
#
# fail closed: 1つでも落ちたら失敗。四半期の定期実行(.github/workflows/audit.yml)と
# 手動(mise run audit:checks)の両方から呼ばれる。変異ドリル(検査を破ってみせる)の
# 手順は docs/conventions/internal-06 §10.10 の台帳を参照。
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

fail=0
run() {
  echo "== $1"
  shift
  if "$@"; then echo "   ok"; else echo "   NG: $*" >&2; fail=1; fi
}

run "affected 判定ツールのテスト" sh -c 'cd dev && GOWORK=off go test ./affected/ >/dev/null'
run "querylint 自身のテスト"      sh -c 'cd dev && GOWORK=off go test ./querylint/ >/dev/null'
run "api-breaking のセルフテスト" dev/scripts/api-breaking.test.sh
run "baseline-diff のセルフテスト" dev/scripts/baseline-diff.test.sh
run "アラートルールの単体テスト"  mise run --quiet lint:alerts
run "昇格ゲート判定のテスト"      python3 -m unittest discover -s infra/hook -p 'index_test.py' -v

if [ "$fail" = 0 ]; then
  echo "検査の検査: すべて期待どおり"
else
  echo "検査の検査: 落ちたものがある(壊れた検査は成功として出てくる。放置しない)" >&2
  exit 1
fi
