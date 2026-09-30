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
TOTP_SECRET=""
totp() { # $TOTP_SECRET(生文字列。Keycloak は ascii bytes を鍵にする)から RFC 6238
  SECRET="$TOTP_SECRET" python3 - <<'PY'
import hashlib, hmac, os, struct, time
key = os.environ["SECRET"].encode()
counter = int(time.time()) // 30
mac = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
o = mac[-1] & 0x0F
print(f"{(struct.unpack('>I', mac[o:o+4])[0] & 0x7FFFFFFF) % 1000000:06d}")
PY
}

pkce() { # code_verifier と S256 challenge
  python3 - <<'PY'
import base64, hashlib, secrets
v = secrets.token_urlsafe(48)
c = base64.urlsafe_b64encode(hashlib.sha256(v.encode()).digest()).rstrip(b"=").decode()
print(v); print(c)
PY
}

form_action() { # form_action <file> — フォームの action を取り出す
  python3 - "$1" <<'PY'
import html, re, sys
s = open(sys.argv[1]).read()
m = re.search(r'<form[^>]+action="([^"]+)"', s)
print(html.unescape(m.group(1)) if m else "")
PY
}

# authorize <acr_values> -> アクセストークンを $ACCESS に。ページ遷移は $tmp/page に残す
authorize() {
  local acr=$1
  local verifier challenge
  { read -r verifier; read -r challenge; } < <(pkce)
  curl -sL -c "$JAR" -b "$JAR" -o "$tmp/page" \
    "$AUTH?client_id=agent&response_type=code&scope=openid&redirect_uri=$REDIRECT&acr_values=$acr&code_challenge=$challenge&code_challenge_method=S256"
  # ログインフォーム(初回)か、SSO 済みなら即リダイレクト。フォームがあれば順に埋める
  local guard=0 location code=""
  while [ $guard -lt 4 ]; do
    guard=$((guard + 1))
    local action
    action=$(form_action "$tmp/page")
    if [ -z "$action" ]; then break; fi
    local data
    if grep -q 'name="totpSecret"' "$tmp/page"; then
      # 初回のみ: TOTP セットアップページ。secret を拾ってエンロールする
      TOTP_SECRET=$(sed -n 's/.*name="totpSecret" value="\([^"]*\)".*/\1/p' "$tmp/page" | head -1)
      [ -n "$TOTP_SECRET" ] || { ng "セットアップページから secret が取れない"; return 1; }
      data="totp=$(totp)&userLabel=dev-totp&totpSecret=$TOTP_SECRET"
    elif grep -q 'name="otp"' "$tmp/page"; then
      data="otp=$(totp)"
    elif grep -q 'name="password"' "$tmp/page"; then
      data="username=alice&password=alice-dev-password"
    else
      ng "知らないフォームが出た: $(grep -o '<form[^>]*>' "$tmp/page" | head -1)"; return 1
    fi
    location=$(curl -s -c "$JAR" -b "$JAR" -o "$tmp/page" -w '%{redirect_url}' \
      -X POST "$action" --data "$data")
    if [ -n "$location" ]; then
      case "$location" in
        "$REDIRECT"*) code=$(printf '%s' "$location" | sed -n 's/.*[?&]code=\([^&]*\).*/\1/p'); break ;;
        *) curl -s -c "$JAR" -b "$JAR" -o "$tmp/page" "$location" ;;
      esac
    fi
  done
  [ -n "$code" ] || { ng "認可コードが取れない(acr=$acr)"; return 1; }
  ACCESS=$(curl -sf -X POST "$TOKEN" \
    -d client_id=agent -d grant_type=authorization_code -d "code=$code" \
    -d "redirect_uri=$REDIRECT" -d "code_verifier=$verifier" \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')
}

acr_of() {
  printf '%s' "$1" | python3 -c '
import sys, json, base64
p = sys.stdin.read().split(".")[1]; p += "=" * (-len(p) % 4)
print(json.loads(base64.urlsafe_b64decode(p)).get("acr", ""))'
}

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
