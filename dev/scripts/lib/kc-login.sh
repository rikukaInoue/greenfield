#!/usr/bin/env bash
# Keycloak の browser-loa フロー(password / +TOTP の step-up)を curl で踏む共有関数群。
# stepup-check.sh と mcp-hitl-check.sh が source する。
#
# 呼び出し側が事前に設定する変数: AUTH, TOKEN(トークンエンドポイント), REDIRECT, tmp, JAR
# 主要関数:
#   authorize <acr_values>  … ブラウザフローを踏んで $ACCESS にアクセストークンを入れる
#   acr_of <jwt>            … acr クレームを出す
# TOTP は初回 step-up のセットアップページから secret を拾って計算する(realm に焼かない)。

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
