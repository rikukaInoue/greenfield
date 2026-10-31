#!/usr/bin/env bash
# worktree ごとに分離した作業場所を作る(#302)。
#
#   dev/wt/new.sh <name> [<ref>]
#
# 分けるもの:
#   - ポート: worktree ごとの番号帯(GREENFIELD_PORT_OFFSET)。ロックを取って決める
#   - frontend の node_modules: APFS の clonefile(cp -c)で複製する
#   - MySQL: sashiki のブランチ。サービスごとの DB ユーザーと GRANT ごと分かれる
#
# worktree ごとの値は <worktree>/mise.local.toml に書く(mise が自動で読む。git 管理外)。
#
# macOS の bash 3.2 で動かす: 連想配列を使わない。変数は ${var} で囲む
# (全角文字が続くと unbound 扱いになるため)。
set -euo pipefail

name=${1:-}
ref=${2:-HEAD}
if [[ ! ${name} =~ ^[a-z0-9][a-z0-9-]{0,23}$ ]]; then
  echo "usage: dev/wt/new.sh <name> [<ref>]   (name は ^[a-z0-9][a-z0-9-]{0,23}$)" >&2
  exit 2
fi

log() { printf '== %s\n' "$*"; }
die() { printf 'wt-new: %s\n' "$*" >&2; exit 1; }

# メインの checkout(どの worktree から呼んでも同じ場所)
main=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
dst="$(dirname "${main}")/greenfield.${name}"
[[ -e ${dst} ]] && die "${dst} は既にある"

# --- 1. ポートの番号帯を決める ------------------------------------------------
# 番号帯は 30000 + slot*200。49152 以上(macOS の一時ポート)と、既存のポート
# (Keycloak 8180 / RustFS 9000 / sashiki 28080・28306・284xx)を避ける。
state=${GREENFIELD_WT_STATE:-${HOME}/.local/state/greenfield-wt}
mkdir -p "${state}/slots"
lock="${state}/lock"
waited=0
until mkdir "${lock}" 2>/dev/null; do
  sleep 0.2
  waited=$((waited + 1))
  [[ ${waited} -gt 150 ]] && die "ロック ${lock} が 30 秒取れない(残骸なら rmdir する)"
done
trap 'rmdir "${lock}" 2>/dev/null || true' EXIT

# 消えた worktree の番号は返す
for f in "${state}"/slots/*; do
  [[ -e ${f} ]] || continue
  [[ -d $(cat "${f}") ]] || rm -f "${f}"
done
slot=""
for i in $(seq 1 55); do
  if [[ ! -e ${state}/slots/${i} ]]; then slot=${i}; break; fi
done
[[ -n ${slot} ]] || die "空いている番号帯が無い(55 本まで)"
echo "${dst}" > "${state}/slots/${slot}"
rmdir "${lock}"
trap - EXIT
offset=$((30000 + slot * 200))
log "番号帯 ${slot}(ポートに ${offset} を足す)"

# --- 2. git worktree --------------------------------------------------------------
if git -C "${main}" show-ref --verify --quiet "refs/heads/${name}"; then
  git -C "${main}" worktree add "${dst}" "${name}" >/dev/null
else
  git -C "${main}" worktree add -b "${name}" "${dst}" "${ref}" >/dev/null
fi
mise trust -q "${dst}" >/dev/null 2>&1 || true
log "worktree ${dst}"

# --- 3. frontend の node_modules を clonefile で複製する ---------------------------
cloned=0
for d in frontend frontend/apps/* frontend/packages/*; do
  src="${main}/${d}/node_modules"
  [[ -d ${src} && -d ${dst}/${d} ]] || continue
  cp -cR "${src}" "${dst}/${d}/"
  cloned=$((cloned + 1))
done
if [[ ${cloned} -gt 0 ]]; then
  if ! cmp -s "${main}/frontend/pnpm-lock.yaml" "${dst}/frontend/pnpm-lock.yaml"; then
    log "pnpm-lock.yaml が複製元と違うので install する"
    (cd "${dst}/frontend" && pnpm install --frozen-lockfile >/dev/null)
  fi
  log "frontend の node_modules を複製(${cloned} か所)"
fi

# --- 4. MySQL: sashiki のブランチ ---------------------------------------------
# プロキシ(28306)はサービスごとの DB ユーザー(photo_app など)を通さないので、
# ブランチの mysqld(engine_port)に直接つなぐ。初期化だけはプロキシ経由の dev で流す。
sashiki_bin=${SASHIKI_BIN:-$(command -v sashiki || echo "${HOME}/gh/sashiki/bin/sashiki")}
export SASHIKI_API_URL=${SASHIKI_API_URL:-http://127.0.0.1:28080}
db_branch="gf-${name}"
eport=""
if [[ -x ${sashiki_bin} && ${WT_DB:-1} != 0 ]]; then
  "${sashiki_bin}" create --exist-ok --purpose "greenfield worktree ${name}" "${db_branch}" >/dev/null
  "${sashiki_bin}" wake "${db_branch}" >/dev/null 2>&1 || true
  pport=$("${sashiki_bin}" env "${db_branch}" | sed -n 's/^DB_PORT=//p')
  eport=$("${sashiki_bin}" show "${db_branch}" --json | python3 -c 'import sys,json;print(json.load(sys.stdin)["engine_port"])')
  pass=${SASHIKI_DB_PASSWORD:-dev}
  for f in "${dst}"/deploy/compose/mysql/init/*.sql; do
    MYSQL_PWD=${pass} mysql -h 127.0.0.1 -P "${pport}" -u "dev@${db_branch}" < "${f}"
  done
  log "sashiki のブランチ ${db_branch}(mysqld 127.0.0.1:${eport})"
else
  log "sashiki が無いので DB はメインと共有する(WT_DB=0 か sashiki 未導入)"
fi

# --- 5. mise.local.toml --------------------------------------------------------
p() { echo $(($1 + offset)); }
{
  echo "# dev/wt/new.sh が書いた worktree 固有の設定(#302)。git 管理外"
  echo "[env]"
  echo "GREENFIELD_WT = \"${name}\""
  echo "GREENFIELD_PORT_OFFSET = \"${offset}\""
  echo "PHOTO_EXTERNAL_ADDR = \":$(p 8080)\""
  echo "PHOTO_INTERNAL_ADDR = \":$(p 8081)\""
  echo "PHOTO_ADMIN_ADDR = \":$(p 8082)\""
  echo "GEAR_EXTERNAL_ADDR = \":$(p 8090)\""
  echo "GEAR_INTERNAL_ADDR = \":$(p 8091)\""
  echo "GEAR_ADMIN_ADDR = \":$(p 8092)\""
  echo "AUTHZ_ADDR = \":$(p 8100)\""
  echo "PHOTO_INTERNAL_URL = \"http://localhost:$(p 8081)\""
  echo "GEAR_INTERNAL_URL = \"http://localhost:$(p 8091)\""
  echo "AUTHZ_URL = \"http://localhost:$(p 8100)\""
  echo "PHOTO_API_URL = \"http://localhost:$(p 8080)\""
  echo "GEAR_API_URL = \"http://localhost:$(p 8090)\""
  if [[ -n ${eport} ]]; then
    echo "SASHIKI_BRANCH = \"${db_branch}\""
    for svc in photo gear; do
      echo "$(echo "${svc}" | tr a-z A-Z)_DSN = \"${svc}_app:${svc}_app@tcp(127.0.0.1:${eport})/${svc}?parseTime=true\""
      echo "$(echo "${svc}" | tr a-z A-Z)_MIGRATE_DSN = \"${svc}_migrate:${svc}_migrate@tcp(127.0.0.1:${eport})/${svc}\""
    done
    echo "LOCALAUTHZ_DSN = \"localauthz:localauthz@tcp(127.0.0.1:${eport})/localauthz\""
  fi
} > "${dst}/mise.local.toml"
mise trust -q "${dst}/mise.local.toml" >/dev/null 2>&1 || true

# --- 6. マイグレーション ---------------------------------------------------------
if [[ -n ${eport} ]]; then
  (
    cd "${dst}"
    for svc in $(mise run -q services 2>/dev/null); do
      n=$(basename "${svc}")
      mise exec -- go run "./${svc}/cmd/${n}" migrate expand >/dev/null
      mise exec -- go run "./${svc}/cmd/${n}" migrate contract >/dev/null
    done
  )
  log "マイグレーション適用済み"
fi

cat <<EOF

作成しました: ${dst}

  cd ${dst}
  mise run run             # allinone: photo :$(p 8080)  gear :$(p 8090)
  portless run --name ${name}.greenfield -- pnpm --dir frontend dev
                           # https://${name}.greenfield.localhost

  片付け: dev/wt/rm.sh ${name}
EOF
