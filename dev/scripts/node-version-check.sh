#!/usr/bin/env bash
# Node ランタイム自体の CVE への備え（#183）。依存をいくら上げても runtime の穴は消えない。
# Go は govulncheck が toolchain の CVE も報告するが、npm 側に相当品が無いので、
# mise.toml でピン留めした node が同メジャーの最新パッチから遅れたら落とす。
# （最新パッチ＝セキュリティ修正を含む。遅れの検出であって、CVE の有無までは見ない）
#
#   dev/scripts/node-version-check.sh
#
# 外部 API（endoflife.date）に依存するので PR ゲートには入れず、security.yml の日次で回す。
set -eu
cd "$(dirname "$0")/../.."

pin=$(grep -E '^node = ' mise.toml | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
if [ -z "$pin" ]; then
  echo "mise.toml から node のピンが読めない。検査が空振りしている" >&2
  exit 1
fi
major=${pin%%.*}

latest=$(curl -sf --max-time 30 https://endoflife.date/api/nodejs.json \
  | python3 -c "import sys,json;print(next((c['latest'] for c in json.load(sys.stdin) if c['cycle']=='$major'), ''))")
if [ -z "$latest" ]; then
  echo "endoflife.date から node $major 系の最新が取れない。検査が空振りしている" >&2
  exit 1
fi

echo "node: ピン $pin / $major 系の最新 $latest"
if [ "$pin" != "$latest" ]; then
  echo "NG: node のピンが最新パッチから遅れている。mise.toml の node を $latest へ上げる（SSR は Node がサーバとして本番に立つ）" >&2
  exit 1
fi
echo "ok: node は $major 系の最新パッチ"
