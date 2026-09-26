#!/usr/bin/env bash
# flagd の sync ポートが応答するまで待つ。
#
# flagd のイメージは distroless でシェルも curl も持たないため、compose の healthcheck を
# 書けない（書けば常に unhealthy になる）。そのため `--wait` が使えず、起動直後に
# アプリを立てると定義を同期できず、宣言した既定値のままプロセスの生涯を過ごす。
# ホスト側から TCP で到達性を確かめることで `--wait` 相当を回復する。
set -euo pipefail
host=${FLAGD_HOST:-localhost}
port=${FLAGD_PORT:-8015}
timeout=${FLAGD_WAIT_SECONDS:-30}

for _ in $(seq 1 "$timeout"); do
  if nc -z "$host" "$port" 2>/dev/null; then
    echo "flagd は ${host}:${port} で応答している"
    exit 0
  fi
  sleep 1
done
echo "flagd が ${timeout} 秒以内に ${host}:${port} で応答しなかった" >&2
exit 1
