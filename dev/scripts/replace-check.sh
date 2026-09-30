#!/usr/bin/env bash
# モジュール間依存の replace と、Dockerfile の COPY の整合を検査する(#163)。
#
# GOWORK=off ビルドで自リポジトリ配下の require に replace が無いと、Go は GitHub の
# 公開リポジトリから published 版を取りに行く。パッケージが無ければ音を立てて落ちる
# (無害)が、**在ると古い公開版を黙って掴んでビルドが通る**。実例が2つ:
#   - #158: dev/go.mod で photo-client の replace が無く published 版を黙って拾っていた
#   - リポジトリの public 化(2026-09-28)でこの経路が恒常化した
#
# 同族の穴として、Dockerfile がその go.mod のローカル replace 先を COPY していないと、
# デプロイイメージだけが壊れる。しかも**次に依存が変わってキャッシュが破れるまで潜伏する**:
#   - gear の Dockerfile が photo-client を COPY しておらず、#158 から #164 まで
#     デプロイイメージはビルド不能だった(誰もリビルドしないため気づけない)
#
# 検査は3点:
#   1. 自リポジトリ配下の require には対応する replace がある
#   2. replace の指す先ディレクトリ(go.mod を持つ)が実在する
#   3. Dockerfile はその go.mod の全ローカル replace 先を COPY している
#
# 前提の検証(fail open にしない): 走査した go.mod / Dockerfile の一覧と件数を必ず出力し、
# 期待する最低数を下回ったら検査自体を失敗させる。
set -euo pipefail
cd "$(dirname "$0")/../.."

MODULE_PREFIX="github.com/rikukaInoue/greenfield"
fail=0
ng() { echo "  NG: $*" >&2; fail=1; }

gomods=$(git ls-files "*go.mod" | grep -v "\.tmpl$")
dockerfiles=$(git ls-files "*Dockerfile")
n_mods=$(echo "$gomods" | grep -c . || true)
n_docker=$(echo "$dockerfiles" | grep -c . || true)
echo "走査対象: go.mod ${n_mods}本 / Dockerfile ${n_docker}本"
if [ "$n_mods" -lt 3 ] || [ "$n_docker" -lt 1 ]; then
  echo "NG: 走査対象が少なすぎる(go.mod ${n_mods}, Dockerfile ${n_docker})。検査の前提が崩れている" >&2
  exit 1
fi

checked_requires=0
for mod in $gomods; do
  dir=$(dirname "$mod")
  # require された自リポジトリ配下のモジュール(コメント・空行・indirect も対象。
  # indirect でも replace が無ければ published 版を掴むのは同じ)
  requires=$(grep -oE "${MODULE_PREFIX}/[a-z0-9/_-]+ v[^ ]+" "$mod" | awk "{print \$1}" | sort -u || true)
  replaces=$(grep -E "^replace ${MODULE_PREFIX}" "$mod" | awk "{print \$2}" | sort -u || true)
  for req in $requires; do
    checked_requires=$((checked_requires + 1))
    if ! echo "$replaces" | grep -qx "$req"; then
      ng "$mod: $req の replace が無い(公開版を黙って拾う)"
      # flaky 調査(#204系: replace が実在するのに稀に見落とす報告が2件)。
      # 再現時に「その瞬間の grep が何を見たか」を残す
      echo "  -- debug: $mod の replace 行 --" >&2
      grep -n "^replace" "$mod" >&2 || echo "  (grep が replace 行を0件返した)" >&2
      echo "  -- debug: 抽出済み replaces --" >&2
      echo "$replaces" >&2
    fi
  done
  # replace の指す先が実在するか
  while read -r line; do
    [ -z "$line" ] && continue
    name=$(echo "$line" | awk "{print \$2}")
    target=$(echo "$line" | awk "{print \$4}")
    case "$target" in
      /*|.*) ;;
      *) continue ;;  # バージョン指定 replace は対象外
    esac
    if [ ! -f "$dir/$target/go.mod" ]; then
      ng "$mod: replace $name => $target の先に go.mod が無い"
    fi
  done <<< "$(grep -E "^replace ${MODULE_PREFIX}" "$mod" || true)"
done

checked_copies=0
for df in $dockerfiles; do
  # Dockerfile はモジュールのディレクトリに置かれ、context はリポジトリルート
  dir=$(dirname "$df")
  mod="$dir/go.mod"
  [ -f "$mod" ] || { ng "$df: 同じディレクトリに go.mod が無い(前提が変わったら検査を直す)"; continue; }
  while read -r line; do
    [ -z "$line" ] && continue
    target=$(echo "$line" | awk "{print \$4}")
    case "$target" in .*) ;; *) continue ;; esac
    # ../../telemetry (from services/x) → telemetry のように repo ルート相対へ正規化
    normalized=$(python3 -c "import os,sys; print(os.path.normpath(os.path.join(\"$dir\", \"$target\")))")
    checked_copies=$((checked_copies + 1))
    # 依存解決層(go.mod)とソース層の**両方**を個別に要求する。片方だけの grep だと、
    # go.mod 層が残っている状態でソース層の消失を見逃す(変異テストで実際にすり抜けた)
    if ! grep -qE "^COPY ${normalized}/go\.mod" "$df"; then
      ng "$df: replace 先 ${normalized} の go.mod を依存解決層で COPY していない"
    fi
    if ! grep -qE "^COPY ${normalized} " "$df"; then
      ng "$df: replace 先 ${normalized} のソースを COPY していない(依存が変わった時に初めて壊れる)"
    fi
  done <<< "$(grep -E "^replace ${MODULE_PREFIX}" "$mod" || true)"
done

if [ "$checked_requires" -lt 1 ] || [ "$checked_copies" -lt 1 ]; then
  echo "NG: 検査した対象が少なすぎる(require ${checked_requires}, COPY ${checked_copies})。前提が崩れている" >&2
  exit 1
fi

if [ "$fail" = 1 ]; then
  echo "" >&2
  echo "replace の書き方はモジュールの go.mod 末尾を、COPY は services/photo/Dockerfile を参照。" >&2
  exit 1
fi
echo "ok: require ${checked_requires}件の replace と、Dockerfile の COPY ${checked_copies}件を確認"
