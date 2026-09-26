#!/usr/bin/env bash
# 空DBへマイグレーション（expand + contract）を全適用し、mysqldump --no-data を正規化して出力する。
# sqlc の入力スキーマ（services/<name>/db/schema.sql）は手書きせず、この出力で更新・検証する。
#
#   dev/scripts/schema-dump.sh <service>              # 標準出力へ
#   dev/scripts/schema-dump.sh <service> --write      # services/<service>/db/schema.sql を更新
#   dev/scripts/schema-dump.sh <service> --check      # 使い捨てDBへ全適用した姿と一致しなければ失敗
#   dev/scripts/schema-dump.sh <service> --check-live # 実行時のDBと一致しなければ失敗
#
# --check は「マイグレーションの列と schema.sql が合っているか」を見る。
# --check-live は「実際に使う DB が schema.sql どおりか」を見る。
# 後者が無いと、contract を流さない環境で sqlc の入力と実行時のスキーマが食い違う（監査 A-2）。
#
# MYSQL_CONTAINER が空なら、ホストの mysql / mysqldump で MYSQL_HOST へ接続する（CI 用）。
set -euo pipefail
svc=${1:?usage: schema-dump.sh <service> [--write|--check]}
mode=${2:-}
container=${MYSQL_CONTAINER-greenfield-mysql}
host=${MYSQL_HOST:-127.0.0.1}
port=${MYSQL_PORT:-3306}
scratch="${svc}_schemacheck"
root=$(cd "$(dirname "$0")/../.." && pwd)
target="$root/services/$svc/db/schema.sql"

if [ -n "$container" ]; then
  mysql_root() { docker exec -i "$container" mysql -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
  mysqldump_root() { docker exec "$container" mysqldump -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
else
  mysql_root() { mysql -h "$host" -P "$port" -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
  mysqldump_root() { mysqldump -h "$host" -P "$port" -uroot -proot "$@" 2> >(grep -v "Using a password" >&2); }
fi

mysql_root -e "DROP DATABASE IF EXISTS \`$scratch\`; CREATE DATABASE \`$scratch\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
trap 'mysql_root -e "DROP DATABASE IF EXISTS \`$scratch\`" || true' EXIT

dsn="root:root@tcp($host:$port)/$scratch"
env_prefix=$(echo "$svc" | tr '[:lower:]' '[:upper:]')
(cd "$root" && env "${env_prefix}_MIGRATE_DSN=$dsn" go run "./services/$svc/cmd/$svc" migrate expand >/dev/null)
(cd "$root" && env "${env_prefix}_MIGRATE_DSN=$dsn" go run "./services/$svc/cmd/$svc" migrate contract >/dev/null)

dump=$(mysqldump_root --no-data --skip-comments --compact \
  --ignore-table="$scratch.${svc}_migrations_expand" --ignore-table="$scratch.${svc}_migrations_contract" "$scratch" \
  | { grep -v '^/\*!' || true; } | sed -E 's/ AUTO_INCREMENT=[0-9]+//')
header="-- 生成物。手で編集しない。更新: dev/scripts/schema-dump.sh $svc --write（マイグレーション全適用後の mysqldump --no-data）"
out="$header"$'\n'"$dump"

case "$mode" in
  --write) printf '%s\n' "$out" > "$target"; echo "wrote $target" ;;
  --check) if diff -u "$target" <(printf '%s\n' "$out"); then echo "schema.sql is up to date (${svc})"; else echo "schema.sql is stale (${svc}): run dev/scripts/schema-dump.sh $svc --write" >&2; exit 1; fi ;;
  --check-live)
    live=$(mysqldump_root --no-data --skip-comments --compact \
      --ignore-table="${svc}.${svc}_migrations_expand" --ignore-table="${svc}.${svc}_migrations_contract" "$svc" \
      | { grep -v '^/\*!' || true; } | sed -E 's/ AUTO_INCREMENT=[0-9]+//')
    if diff -u "$target" <(printf '%s\n%s\n' "$header" "$live"); then
      echo "実行時の DB は schema.sql と一致している (${svc})"
    else
      echo "実行時の DB が schema.sql と食い違っている (${svc}): mise run migrate で contract まで流す" >&2
      exit 1
    fi
    ;;
  "") printf '%s\n' "$out" ;;
  *) echo "unknown mode $mode" >&2; exit 2 ;;
esac
