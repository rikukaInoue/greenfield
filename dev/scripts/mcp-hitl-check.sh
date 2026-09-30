#!/usr/bin/env bash
# MCP エージェントの human-in-the-loop 強制の E2E(check #14 / ステージ 5.4)。
#
# 確かめること:
#   1. エージェント(aal1 トークン)が MCP の delete_account ツールを呼ぶ → ツールは
#      401 の step-up チャレンジを isError で返す。エージェントは自力で進めない
#   2. エージェントは非対話で aal2 を取れない(acr_values=aal2 の direct grant を OP が拒む)
#      = human-in-the-loop の抜け穴が無いことの確認
#   3. 人間が TOTP で aal2 トークンを取得 → 同じツール呼び出しが成功する
#   4. photo の監査ログに actor.client_id が載る(誰が/何が叩いたか。401 で止まった試行も)
#
# 前提: keycloak(8180, browser-loa + TOTP) / authz(8100,OIDC) / photo(8082,OIDCDeps) 稼働。
set -eu
cd "$(cd "$(dirname "$0")/../.." && pwd)"

KC="${KEYCLOAK_URL:-http://localhost:8180}"
AUTH="$KC/realms/greenfield/protocol/openid-connect/auth"
TOKEN="$KC/realms/greenfield/protocol/openid-connect/token"
ADMIN="${PHOTO_ADMIN_URL:-http://localhost:8082}"
AUTHZ="${AUTHZ_URL:-http://localhost:8100}"
REDIRECT="http://localhost:9977/cb"
PHOTO_LOG_GLOB="${PHOTO_LOG:-$HOME/.claude/jobs/*/tmp/su-photo.log}"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
JAR="$tmp/cookies"
source "$(dirname "$0")/lib/kc-login.sh"

MCP="$tmp/mcpadmin"
( cd dev && GOWORK=off go build -o "$MCP" ./mcpadmin )

sub_of() { printf '%s' "$1" | python3 -c 'import sys,json,base64
t=sys.stdin.read();p=t.split(".")[1];p+="="*(-len(p)%4)
print(json.loads(base64.urlsafe_b64decode(p))["sub"])'; }

# MCP サーバに initialize → tools/call を送り、結果テキストを "OK ..."/"ERR ..." で返す
mcp_call() { # mcp_call <token> <subject>
  { printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}\n'
    printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_account","arguments":{"subject":"%s"}}}\n' "$2"
  } | ADMIN_URL="$ADMIN" ADMIN_TOKEN="$1" "$MCP" \
    | python3 -c 'import sys,json
for line in sys.stdin:
    d=json.loads(line)
    if d.get("id")==2:
        r=d["result"]
        print(("ERR " if r.get("isError") else "OK ")+r["content"][0]["text"].replace(chr(10)," / "))'
}

echo "0. エージェントに aal1 トークン + operator 権限を用意(AAL だけが足りない状態)"
authorize aal1
ALICE_AAL1=$ACCESS
[ "$(acr_of "$ALICE_AAL1")" = aal1 ] || { echo "aal1 が取れない" >&2; exit 1; }
SUB=$(sub_of "$ALICE_AAL1")
PLATFORM=$(curl -s -X POST "$TOKEN" -d client_id=svc-photo -d client_secret=svc-photo-dev-secret \
  -d grant_type=client_credentials -d scope=internal:platform \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))')
curl -sf -X POST "$AUTHZ/tuples:write" -H "Authorization: Bearer $PLATFORM" -H "Content-Type: application/json" \
  -d "{\"writes\":[{\"subject\":\"user:$SUB\",\"relation\":\"operator\",\"object\":\"platform:main\"}]}" >/dev/null
ok "aal1 + operator を用意(sub=$SUB)"

echo "1. エージェントが MCP delete_account を呼ぶ(aal1)"
R=$(mcp_call "$ALICE_AAL1" "probe-hitl")
echo "   → $R"
printf '%s' "$R" | grep -q '^ERR' && ok "ツールは isError で止まった" || ng "止まらない: $R"
printf '%s' "$R" | grep -q 'insufficient_user_authentication' && ok "step-up チャレンジを返す" || ng "チャレンジが無い"
printf '%s' "$R" | grep -q '人間' && ok "人間の再認証を促す(human-in-the-loop)" || ng "促していない"

echo "2. エージェントは非対話で aal2 を取れない(抜け穴の不在)"
A2=$(curl -s -X POST "$TOKEN" -d client_id=agent -d grant_type=password \
  -d username=alice -d password=alice-dev-password -d scope=openid -d acr_values=aal2 \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))')
if [ -z "$A2" ]; then
  ok "acr_values=aal2 の direct grant は拒否される(自己昇格できない)"
elif [ "$(acr_of "$A2")" = aal2 ]; then
  ng "aal2 を非対話で取れてしまった(human-in-the-loop の抜け穴)"
else
  ok "トークンは出たが aal2 に上がらない(acr=$(acr_of "$A2"))"
fi

echo "3. 人間が TOTP で aal2 トークンを取得 → 同じツールが成功する"
authorize aal2
AAL2=$ACCESS
[ "$(acr_of "$AAL2")" = aal2 ] && ok "人間の step-up で aal2 トークン取得" || ng "aal2 に上がらない"
R=$(mcp_call "$AAL2" "probe-hitl")
echo "   → $R"
printf '%s' "$R" | grep -q '^OK' && ok "再認証後は同じツール呼び出しが成功する" || ng "成功しない: $R"

echo "4. 監査ログに actor.client_id が載る(誰が/何が叩いたか)"
LOGFILE=$(ls $PHOTO_LOG_GLOB 2>/dev/null | head -1)
if [ -n "$LOGFILE" ] && grep -q 'admin.account.delete.attempt' "$LOGFILE"; then
  grep -q '"actor.client_id":"agent"' "$LOGFILE" \
    && ok "監査に client_id=agent(機械が叩いた証跡)" || ng "client_id=agent が監査に無い"
  grep -q '"audit.event":"admin.account.delete.challenged"' "$LOGFILE" \
    && ok "401 で止まった試行も監査に残る" || ng "challenged が監査に無い"
  grep -q '"audit.event":"admin.account.delete.completed"' "$LOGFILE" \
    && ok "成功も監査に残る" || ng "completed が監査に無い"
else
  ng "監査ログが見つからない($PHOTO_LOG_GLOB)"
fi

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; exit 1; fi
