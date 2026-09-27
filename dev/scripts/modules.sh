#!/usr/bin/env bash
# go.work に登録されたモジュールを1行ずつ出力する。
#
#   dev/scripts/modules.sh             # 全モジュール（./core 形式）
#   dev/scripts/modules.sh --services  # サービスモジュールのみ（services/photo 形式。*-client を除く）
#
# MODULES（空白区切り、./core 形式）があればその範囲に絞る。go.work に無いものを指定したら失敗する
# （打ち間違いで対象0件のまま検査が通るのを防ぐ）。
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
all=$(awk '/^\t\.\//{print $1}' "$root/go.work")

selected=$all
if [ -n "${MODULES:-}" ]; then
  selected=""
  for m in $MODULES; do
    if ! grep -qx -- "$m" <<<"$all"; then
      echo "modules.sh: go.work に無いモジュール: $m" >&2
      exit 1
    fi
    selected+="$m"$'\n'
  done
fi

case "${1:-}" in
  "") printf '%s' "$selected" | sed '/^$/d' ;;
  --services) printf '%s' "$selected" | { grep -E '^\./services/[a-z0-9]+$' || true; } | sed 's#^\./##' ;;
  *) echo "usage: modules.sh [--services]" >&2; exit 2 ;;
esac
