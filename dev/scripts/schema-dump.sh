#!/usr/bin/env bash
# 空DBへマイグレーション（expand + contract）を全適用し、mysqldump --no-data を正規化して出力する。
# sqlc の入力スキーマ（services/<name>/db/schema.sql）は手書きせず、この出力で更新・検証する
# （conventions/internal-05「sqlcとの整合」）。
#
#   dev/scripts/schema-dump.sh <service>            # 標準出力へ
#   dev/scripts/schema-dump.sh <service> --write    # services/<service>/db/schema.sql を更新
#   dev/scripts/schema-dump.sh <service> --check    # 一致しなければ diff を出して失敗
set -euo pipefail
svc=${1:?usage: schema-dump.sh <service> [--write|--check]}
mode=${2:-}
container=${MYSQL_CONTAINER:-greenfield-mysql}
scratch="${svc}_schemacheck"
root=$(cd "$(dirname "$0")/../.." && pwd)
target="$root/services/$svc/db/schema.sql"

mysql_root() { docker exec -i "$container" mysql -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }

mysql_root -e "DROP DATABASE IF EXISTS \`$scratch\`; CREATE DATABASE \`$scratch\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
trap 'mysql_root -e "DROP DATABASE IF EXISTS \`$scratch\`" || true' EXIT

dsn="root:root@tcp(127.0.0.1:3306)/$scratch"
env_prefix=$(echo "$svc" | tr '[:lower:]' '[:upper:]')
(cd "$root" && env "${env_prefix}_MIGRATE_DSN=$dsn" go run "./services/$svc/cmd/$svc" migrate expand >/dev/null)
(cd "$root" && env "${env_prefix}_MIGRATE_DSN=$dsn" go run "./services/$svc/cmd/$svc" migrate contract >/dev/null)

dump=$(docker exec "$container" mysqldump -uroot -proot --no-data --skip-comments --compact \
  --ignore-table="$scratch.${svc}_migrations_expand" --ignore-table="$scratch.${svc}_migrations_contract" "$scratch" 2>/dev/null \
  | grep -v '^/\*!' | sed -E 's/ AUTO_INCREMENT=[0-9]+//')
header="-- 生成物。手で編集しない。更新: dev/scripts/schema-dump.sh $svc --write（マイグレーション全適用後の mysqldump --no-data）"
out="$header"$'\n'"$dump"

case "$mode" in
  --write) printf '%s\n' "$out" > "$target"; echo "wrote $target" ;;
  --check) if diff -u "$target" <(printf '%s\n' "$out"); then echo "schema.sql is up to date ($svc)"; else echo "schema.sql is stale ($svc): run dev/scripts/schema-dump.sh $svc --write" >&2; exit 1; fi ;;
  "") printf '%s\n' "$out" ;;
  *) echo "unknown mode $mode" >&2; exit 2 ;;
esac
