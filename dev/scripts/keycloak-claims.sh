#!/usr/bin/env bash
# Keycloak が realm.json どおりのクレームを載せるかを確かめる（check #13）。
#
# realm-as-code の主張は「realm.json を import すれば毎回同じ状態になる」こと。
# 主張が成立しているかは、立ち上げたあとトークンを取ってクレームを見るしかない。
# Admin Console で手を入れて export を忘れると、ここが落ちる。
#
#   mise run keycloak:check
set -eu

KC="${KEYCLOAK_URL:-http://localhost:8180}"
REALM="${KEYCLOAK_REALM:-greenfield}"
TOKEN_URL="$KC/realms/$REALM/protocol/openid-connect/token"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

# claim <token> <name> -> 値を出す。無ければ空
claim() {
  printf '%s' "$1" | python3 -c '
import sys,json,base64
t=sys.stdin.read().strip()
try:
    p=t.split(".")[1]; p+="="*(-len(p)%4)
    c=json.loads(base64.urlsafe_b64decode(p))
except Exception:
    sys.exit(0)
v=c.get(sys.argv[1])
print("" if v is None else (json.dumps(v, ensure_ascii=False) if isinstance(v,(list,dict)) else v))
' "$2"
}

get_token() {
  local body
  body=$(curl -sf -X POST "$TOKEN_URL" "$@" || true)
  printf '%s' "$body" | python3 -c '
import sys,json
try: print(json.load(sys.stdin).get("access_token",""))
except Exception: print("")
'
}

echo "Keycloak: $KC / realm: $REALM"

if ! curl -sf -o /dev/null "$KC/realms/$REALM"; then
  echo "realm に到達できない。先に起動すること: mise run keycloak:up" >&2
  exit 1
fi

echo
echo "1. ユーザトークン（認可コードの代わりに direct grant で取得）"
UT=$(get_token -d client_id=agent -d grant_type=password \
      -d username=alice -d password=alice-dev-password -d scope=openid)
[ -n "$UT" ] || { ng "トークンが取得できない"; }
if [ -n "$UT" ]; then
  [ "$(claim "$UT" org)" = "rikuka" ] && ok "org=rikuka（user-attribute mapper）" || ng "org が載っていない: '$(claim "$UT" org)'"
  [ -n "$(claim "$UT" amr)" ] && ok "amr キーが存在（値は $(claim "$UT" amr)）" || ng "amr キーが無い"
  [ "$(claim "$UT" iss)" = "$KC/realms/$REALM" ] && ok "iss が一致" || ng "iss がずれている: $(claim "$UT" iss)"
fi

echo
echo "2. M2M トークン（client_credentials + internal:* スコープ）"
for pair in "svc-photo internal:gear" "svc-gear internal:photo"; do
  set -- $pair
  MT=$(get_token -d "client_id=$1" -d "client_secret=$1-dev-secret" \
        -d grant_type=client_credentials -d "scope=$2")
  if [ -z "$MT" ]; then ng "$1 のトークンが取得できない"; continue; fi
  [ "$(claim "$MT" org)" = "greenfield" ] && ok "$1: org=greenfield（hardcoded mapper）" || ng "$1: org が載っていない"
  case " $(claim "$MT" scope) " in
    *" $2 "*) ok "$1: scope に $2 が含まれる" ;;
    *) ng "$1: scope に $2 が無い: $(claim "$MT" scope)" ;;
  esac
done

echo
if [ "$fail" = 0 ]; then
  echo "すべて期待どおり"
else
  echo "期待と違う結果がある。realm.json と実際の realm がずれている可能性がある" >&2
fi
exit "$fail"
