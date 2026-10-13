#!/usr/bin/env bash
# api/<service>/internal.openapi.json から services/<service>-client/client.gen.go を oapi-codegen で生成する。
# admin.openapi.json に経路があれば services/<service>-client/admin/client.gen.go も生成する(#277)。
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
  else
    cp "$tmp/$name.gen.go" "$client/client.gen.go"
    echo "wrote services/$name-client/client.gen.go"
  fi

  # admin リスナーのクライアント(#277)。管理画面(platform/admin)が operator 本人のトークンで
  # 代理呼び出しするための生成物。経路を持つ admin スペックがあるサービスだけ生成する
  # (gear の admin は経路が無く、空のクライアントを置く意味が無い)。
  # パッケージは services/<name>-client/admin(package admin)。internal 側と同じく手書き禁止
  admin_spec="$root/api/$name/admin.openapi.json"
  if [ -f "$admin_spec" ] && python3 -c "import json,sys; sys.exit(0 if json.load(open(sys.argv[1])).get('paths') else 1)" "$admin_spec"; then
    (cd "$root/dev" && GOWORK=off go tool oapi-codegen -generate types,client -package admin "$admin_spec") > "$tmp/$name.admin.gen.go"
    if [ "$mode" = --check ]; then
      cmp -s "$tmp/$name.admin.gen.go" "$client/admin/client.gen.go" || stale+=("services/$name-client/admin/client.gen.go")
    else
      mkdir -p "$client/admin"
      cp "$tmp/$name.admin.gen.go" "$client/admin/client.gen.go"
      echo "wrote services/$name-client/admin/client.gen.go"
    fi
  fi
  [ "$mode" = --check ] || (cd "$client" && GOWORK=off go mod tidy)
done

if [ ${#stale[@]} -gt 0 ]; then
  echo "client-gen: 生成クライアントがスペックと食い違う: ${stale[*]}" >&2
  echo "  再生成: mise run client" >&2
  exit 1
fi
[ "$mode" = --check ] && echo "client-gen: $(wc -w <<<"$services" | tr -d ' ') サービスの生成クライアントが最新"
exit 0
