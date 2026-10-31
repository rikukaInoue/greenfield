#!/usr/bin/env bash
# dev/wt/new.sh で作った作業場所を片付ける(#302)。
#
#   dev/wt/rm.sh <name> [--force]
#
# worktree・sashiki のブランチ・ポートの番号帯を返す。
# コミットしていない変更がある worktree は、--force が無ければ消さない。
set -euo pipefail

name=${1:-}
force=${2:-}
[[ -n ${name} ]] || { echo "usage: dev/wt/rm.sh <name> [--force]" >&2; exit 2; }

log() { printf '== %s\n' "$*"; }
die() { printf 'wt-rm: %s\n' "$*" >&2; exit 1; }

main=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
dst="$(dirname "${main}")/greenfield.${name}"
[[ ${dst} != "${main}" ]] || die "メインの checkout は消さない"

if [[ -d ${dst} ]]; then
  if [[ ${force} != --force && -n $(git -C "${dst}" status --porcelain --untracked-files=no) ]]; then
    die "${dst} にコミットしていない変更がある(消すなら --force)"
  fi
  if [[ ${force} == --force ]]; then
    git -C "${main}" worktree remove --force "${dst}"
  else
    git -C "${main}" worktree remove "${dst}"
  fi
  log "worktree を削除 ${dst}"
fi

sashiki_bin=${SASHIKI_BIN:-$(command -v sashiki || echo "${HOME}/gh/sashiki/bin/sashiki")}
export SASHIKI_API_URL=${SASHIKI_API_URL:-http://127.0.0.1:28080}
if [[ -x ${sashiki_bin} ]] && "${sashiki_bin}" show "gf-${name}" >/dev/null 2>&1; then
  "${sashiki_bin}" delete "gf-${name}" >/dev/null
  log "sashiki のブランチ gf-${name} を削除"
fi

state=${GREENFIELD_WT_STATE:-${HOME}/.local/state/greenfield-wt}
for f in "${state}"/slots/*; do
  [[ -e ${f} ]] || continue
  if [[ $(cat "${f}") == "${dst}" ]]; then
    rm -f "${f}"
    log "番号帯 $(basename "${f}") を返却"
  fi
done

log "ブランチ ${name} は残している(不要なら git branch -D ${name})"
