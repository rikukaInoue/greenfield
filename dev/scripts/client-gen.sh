#!/usr/bin/env bash
# api/<service>/internal.openapi.json から services/<service>-client/client.gen.go を oapi-codegen で生成する。
#
#   dev/scripts/client-gen.sh          # 生成して書き込む
#   dev/scripts/client-gen.sh --check  # 生成物がスペックと一致しなければ失敗する（CI）
#
# 対象は MODULES で絞れる（dev/scripts/modules.sh --services）。client モジュールが無いサービスは失敗する。
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
mode=${1:-}
case "$mode" in "" | --check) ;; *) echo "usage: client-gen.sh [--check]" >&2; exit 2 ;; esac

services=$("$root/dev/scripts/modules.sh" --services)
if [ -z "$services" ]; then
  echo "client-gen: 対象サービスなし"
  exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
stale=()
for s in $services; do
  name=$(basename "$s")
  spec="$root/api/$name/internal.openapi.json"
  client="$root/services/$name-client"
  [ -f "$spec" ] || { echo "client-gen: $spec が無い（mise run api）" >&2; exit 1; }
  [ -d "$client" ] || { echo "client-gen: $client が無い" >&2; exit 1; }
  pkg=$(sed -n 's/^package //p' "$client/doc.go")
  (cd "$root/dev" && GOWORK=off go tool oapi-codegen -generate types,client -package "$pkg" "$spec") > "$tmp/$name.gen.go"
  if [ "$mode" = --check ]; then
    cmp -s "$tmp/$name.gen.go" "$client/client.gen.go" || stale+=("services/$name-client/client.gen.go")
    continue
  fi
  cp "$tmp/$name.gen.go" "$client/client.gen.go"
  (cd "$client" && GOWORK=off go mod tidy)
  echo "wrote services/$name-client/client.gen.go"
done

if [ ${#stale[@]} -gt 0 ]; then
  echo "client-gen: 生成クライアントがスペックと食い違う: ${stale[*]}" >&2
  echo "  再生成: mise run client" >&2
  exit 1
fi
[ "$mode" = --check ] && echo "client-gen: $(wc -w <<<"$services" | tr -d ' ') サービスの生成クライアントが最新"
exit 0
