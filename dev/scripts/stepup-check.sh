#!/usr/bin/env bash
# ステップアップ認証の E2E(check #12 / ステージ 5.3)。
#
# 確かめること(RFC 9470):
#   1. aal1 のトークンで削除 API → 401 + WWW-Authenticate:
#      Bearer error="insufficient_user_authentication", acr_values="aal2"
#   2. チャレンジの acr_values で再認可(同じ SSO セッションのまま TOTP **のみ**追加)
#      → acr=aal2 のトークン → 同じ操作が成功
#
# 前提: keycloak(8180。realm に browser-loa フローと alice の TOTP が import 済み)、
#       openfga(8280) / mysql(13306) / authz(8100, OIDC) / photo(8080-8082, OIDCDeps)。
# ブラウザフローを curl(cookie jar + フォーム POST)で踏む。TOTP は realm.json の
# dev 用シークレットから RFC 6238 で計算する。
set -eu
cd "$(cd "$(dirname "$0")/../.." && pwd)"

KC="${KEYCLOAK_URL:-http://localhost:8180}"
REALM=greenfield
AUTH="$KC/realms/$REALM/protocol/openid-connect/auth"
TOKEN="$KC/realms/$REALM/protocol/openid-connect/token"
ADMIN="${PHOTO_ADMIN_URL:-http://localhost:8082}"
AUTHZ="${AUTHZ_URL:-http://localhost:8100}"
REDIRECT="http://localhost:9977/cb"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

tmp=${STEPUP_TMP:-$(mktemp -d)}; [ -n "${STEPUP_TMP:-}" ] || trap 'rm -rf "$tmp"' EXIT
JAR="$tmp/cookies"

# TOTP は realm に焼かない: otp を credentials に混ぜると **password ごと
# インポートが静かに壊れる**(実測。invalid_user_credentials)。初回の step-up で
# Keycloak が出すセットアップページから secret(hidden の totpSecret)を拾い、
# 以後はそれで計算する——エンロールまで含めた E2E になる。
source "$(dirname "$0")/lib/kc-login.sh"


echo "0. 準備: alice を platform operator にする(認可は満たした上で、保証レベルだけ足りない状態を作る)"
PLATFORM=$(curl -sf -X POST "$TOKEN" -d client_id=svc-photo -d client_secret=svc-photo-dev-secret \
  -d grant_type=client_credentials -d scope=internal:platform \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')
SUB=$(curl -sf -X POST "$TOKEN" -d client_id=agent -d grant_type=password -d username=alice \
  -d password=alice-dev-password -d scope=openid \
  | python3 -c 'import sys,json,base64;t=json.load(sys.stdin)["access_token"];p=t.split(".")[1];p+="="*(-len(p)%4);print(json.loads(base64.urlsafe_b64decode(p))["sub"])')
curl -sf -X POST "$AUTHZ/tuples:write" -H "Authorization: Bearer $PLATFORM" -H "Content-Type: application/json" \
  -d "{\"writes\":[{\"subject\":\"user:$SUB\",\"relation\":\"operator\",\"object\":\"platform:main\"}]}" > /dev/null
ok "operator タプルを書いた(sub=$SUB)"

echo "1. aal1 でログイン(パスワードのみ)"
authorize aal1
[ "$(acr_of "$ACCESS")" = "aal1" ] && ok "acr=aal1 のトークンを取得" || ng "acr が aal1 でない: $(acr_of "$ACCESS")"
AAL1_TOKEN=$ACCESS

echo "2. aal1 トークンで削除 API → RFC 9470 のチャレンジ"
H=$(curl -s -D - -o "$tmp/body" -w '%{http_code}' -X POST "$ADMIN/accounts/probe-nobody:delete" \
  -H "Authorization: Bearer $AAL1_TOKEN")
C=$(printf '%s' "$H" | tail -1)
[ "$C" = 401 ] && ok "401 で止まる" || ng "止まらない: $C $(head -c 200 "$tmp/body")"
CH=$(printf '%s' "$H" | grep -i '^www-authenticate:' | tr -d '\r')
echo "   challenge: ${CH#*: }"
printf '%s' "$CH" | grep -q 'insufficient_user_authentication' && ok "error=insufficient_user_authentication" || ng "エラーコードが違う"
printf '%s' "$CH" | grep -q 'acr_values="aal2"' && ok 'acr_values="aal2" を要求している' || ng "acr_values が無い"

echo "3. チャレンジどおり acr_values=aal2 で再認可(同一セッション。追加入力は TOTP のみのはず)"
authorize aal2
[ "$(acr_of "$ACCESS")" = "aal2" ] && ok "acr=aal2 へ昇格したトークンを取得" || ng "acr: $(acr_of "$ACCESS")"
grep -q 'name="password"' "$tmp/page" 2>/dev/null && ng "再認可でパスワードを再入力させられた(TOTP のみのはず)" || ok "追加入力は TOTP のみ(パスワード再入力なし)"

echo "4. aal2 トークンで同じ操作 → 成功"
C=$(curl -s -o "$tmp/body" -w '%{http_code}' -X POST "$ADMIN/accounts/probe-nobody:delete" \
  -H "Authorization: Bearer $ACCESS")
[ "$C" = 200 ] && ok "削除 API が 200(deleted: $(cat "$tmp/body"))" || ng "成功しない: $C $(head -c 200 "$tmp/body")"

echo "5. 破ってみせる: aal1 のままでは何度やっても通らない"
C=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$ADMIN/accounts/probe-nobody:delete" \
  -H "Authorization: Bearer $AAL1_TOKEN")
[ "$C" = 401 ] && ok "aal1 は引き続き 401(トークンの使い回しで昇格しない)" || ng "aal1 が通ってしまった: $C"

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; exit 1; fi
