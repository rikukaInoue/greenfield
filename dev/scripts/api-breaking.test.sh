#!/usr/bin/env bash
# api-breaking.sh の規則（docs/adr/0017）を、違反を1件ずつ作って検査する。
#
#   dev/scripts/api-breaking.test.sh
#
# 一時ディレクトリに git リポジトリを作り、base のスペックをコミットしてから作業ツリーを書き換える。
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# 一時リポジトリには mise の設定が無いので、このリポジトリで固定した oasdiff を PATH に通す
oasdiff_bin=$(cd "$here" && mise which oasdiff) || { echo "api-breaking.test: oasdiff が見つからない" >&2; exit 1; }
export PATH="$(dirname "$oasdiff_bin"):$PATH"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# spec <version> <prefix> [deprecated] [with-caption]
# GET <prefix>/things を1つ持つスペックを出力する。with-caption=0 で応答の必須項目 caption を消す（破壊的変更）。
spec() {
  python3 - "$@" <<'PY'
import json, sys
version, prefix = sys.argv[1], sys.argv[2]
deprecated = len(sys.argv) > 3 and sys.argv[3] == "1"
caption = not (len(sys.argv) > 4 and sys.argv[4] == "0")
props = {"id": {"type": "integer"}}
required = ["id"]
if caption:
    props["caption"] = {"type": "string"}
    required.append("caption")
op = {"operationId": "ListThings", "responses": {"200": {"description": "OK", "content": {"application/json": {"schema": {"type": "object", "properties": props, "required": required}}}}}}
if deprecated:
    op["deprecated"] = True
print(json.dumps({"openapi": "3.1.0", "info": {"title": "demo", "version": version}, "paths": {f"{prefix}/things": {"get": op}}}, indent=2))
PY
}

cd "$work"
git init -q && git config user.email t@example.com && git config user.name test
mkdir -p dev/scripts api/demo
cp "$here/api-breaking.sh" dev/scripts/
spec 1.0.0 "" > api/demo/external.openapi.json
git add -A && git commit -qm base
base_v1=$(git rev-parse HEAD)

# v1 を全部 deprecated にした base（廃止の検査用）
spec 1.0.0 "" 1 > api/demo/external.openapi.json
spec 2.0.0 /v2 > api/demo/external.v2.openapi.json
git add -A && git commit -qm deprecated
base_deprecated=$(git rev-parse HEAD)

failures=0
# check <名前> <期待: pass|fail> <base> [失敗理由に含まれるべき文字列]
check() {
  local name=$1 want=$2 ref=$3 reason=${4:-} out got
  out=$(dev/scripts/api-breaking.sh "$ref" 2>&1)
  if [ $? -eq 0 ]; then got=pass; else got=fail; fi
  if [ "$got" = "$want" ] && { [ -z "$reason" ] || grep -qF -- "$reason" <<<"$out"; }; then
    echo "ok   $name ($got)"
  else
    echo "FAIL $name: got $got, want $want${reason:+（理由: $reason）}"
    echo "$out" | sed 's/^/     /'
    failures=$((failures + 1))
  fi
}
# reset は作業ツリーを ref の内容に戻す（HEAD は動かさない）
reset() { git read-tree "$1" && git checkout-index -fa && git clean -qfd; }

reset "$base_v1"; check "変更なし" pass "$base_v1"

reset "$base_v1"; spec 1.0.0 "" 0 0 > api/demo/external.openapi.json
check "v1 への破壊的変更" fail "$base_v1" "既存のメジャーへの破壊的変更は通さない"

reset "$base_v1"; spec 2.0.0 "" 0 0 > api/demo/external.openapi.json
check "v1 への破壊的変更 + メジャーを 2 に上げる（旧来の抜け道）" fail "$base_v1" "ファイル名のメジャー 1 と食い違う"

reset "$base_v1"; spec 2.0.0 /v2 0 0 > api/demo/external.v2.openapi.json
check "v2 を並行提供（v1 は無変更）" pass "$base_v1"

reset "$base_v1"; spec 2.0.0 "" 0 0 > api/demo/external.v2.openapi.json
check "v2 のパスに /v2 接頭辞が無い" fail "$base_v1" "パスの接頭辞が規則に合わない"

reset "$base_v1"; spec 3.0.0 /v2 > api/demo/external.v2.openapi.json
check "v2 のファイルに 3.0.0" fail "$base_v1" "ファイル名のメジャー 2 と食い違う"

reset "$base_v1"; spec 3.0.0 /v3 > api/demo/external.v3.openapi.json
check "v2 を飛ばして v3 を足す" fail "$base_v1" "1つ前の api/demo/external.v2.openapi.json が無い"

reset "$base_v1"; rm api/demo/external.openapi.json; spec 2.0.0 /v2 > api/demo/external.v2.openapi.json
check "v2 を足して v1 を消す（予告なし）" fail "$base_v1" "deprecated でない操作が 1 件ある"

reset "$base_deprecated"; rm api/demo/external.openapi.json
check "deprecated 済みの v1 を消す" pass "$base_deprecated"

reset "$base_v1"; spec 1.1.0 "" > api/demo/external.openapi.json; spec 1.1.0 "" > api/demo/external.v1.openapi.json
check "external.v1.openapi.json という名前" fail "$base_v1" "名前が規則に合わない"

if [ "$failures" -gt 0 ]; then
  echo "api-breaking.test: ${failures} 件が期待と違う" >&2
  exit 1
fi
echo "api-breaking.test: すべて期待どおり"
