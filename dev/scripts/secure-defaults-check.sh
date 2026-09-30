#!/usr/bin/env bash
# 守りの既定値の検査(8.1 / #210 #212 #213)。規約でなく CI で止める。
#
#   1. コンテナは非 root で動く(全 Dockerfile の最終ステージに USER)
#   2. DB リソースは保存時暗号化(全 aws_db_instance / aws_rds_cluster に storage_encrypted = true)
#   3. 秘密を taskdef の environment に平文で置かない(tf の environment ブロックに
#      db_password / devtoken を参照する行が無い)
#
# fail closed: 走査対象が 0 件なら検査は成立していないので失敗する。
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

fail=0
ng() { echo "NG: $*" >&2; fail=1; }

# --- 1. 非 root ---
dockerfiles=$(ls services/*/Dockerfile platform/*/Dockerfile 2>/dev/null)
n=0
for f in $dockerfiles; do
  n=$((n + 1))
  # 最終ステージ(最後の FROM 以降)に USER があること
  last=$(awk '/^FROM /{ buf="" } { buf = buf $0 "\n" } END { printf "%s", buf }' "$f")
  echo "$last" | grep -q '^USER ' || ng "$f: 最終ステージに USER が無い(root で動く)"
  echo "$last" | grep -q '^USER root' && ng "$f: USER root は非 root にならない"
done
[ "$n" -gt 0 ] || ng "Dockerfile が1つも見つからない(走査が空振り)"

# --- 2. 保存時暗号化 ---
m=0
while IFS= read -r tf; do
  while IFS= read -r line; do
    m=$((m + 1))
    name=$(echo "$line" | cut -d: -f2)
    # リソースブロック内に storage_encrypted = true があること(ブロックは閉じ括弧まで)
    awk -v start="$name" 'NR>=start && /^}/{exit} NR>=start{print}' "$tf" \
      | grep -q 'storage_encrypted[[:space:]]*=[[:space:]]*true' \
      || ng "$tf:$name: storage_encrypted = true が無い"
  done < <(grep -n 'resource "aws_db_instance"\|resource "aws_rds_cluster"' "$tf" | cut -d: -f1 | sed "s|^|$tf:|" | cut -d: -f2 | sed 's/^/&/' )
done < <(find infra -name '*.tf' -not -path '*/.terraform/*')
[ "$m" -gt 0 ] || ng "DB リソースが1つも見つからない(走査が空振り)"

# --- 3. 平文の秘密 ---
t=0
while IFS= read -r tf; do
  t=$((t + 1))
  # environment ブロック(taskdef の env 配列と Lambda の variables)に秘密の参照が無いこと
  hits=$(grep -nE 'name = "[A-Z_]*"?.*(var\.db_password|var\.hook_devtoken)|DEVTOKEN[[:space:]]*=[[:space:]]*var\.' "$tf" | grep -v 'aws_ssm_parameter' || true)
  [ -z "$hits" ] || ng "$tf: environment に秘密が平文で載っている: $hits"
done < <(find infra -name '*.tf' -not -path '*/.terraform/*')
[ "$t" -gt 0 ] || ng "tf が1つも見つからない(走査が空振り)"

if [ "$fail" = 0 ]; then
  echo "ok: Dockerfile ${n}本(非root) / DBリソース ${m}件(暗号化) / tf ${t}本(平文秘密なし) を確認"
else
  exit 1
fi
