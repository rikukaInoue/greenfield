#!/usr/bin/env bash
# 動いている Keycloak の realm を deploy/compose/keycloak/realm.json へ書き戻す。
#
# realm-as-code の運用は「Admin Console で手を入れたら export して JSON へ戻す」
# （docs/03-platform.md）。手順を口伝にすると必ず腐るのでスクリプトにしてある。
#
#   mise run keycloak:export
#
# partial-export は次の2つを返さないので、ここで補っている。
#   - クライアントシークレット（`**********` で伏せられる）
#   - 通常ユーザ（service account だけが含まれる）
set -eu

KC="${KEYCLOAK_URL:-http://localhost:8180}"
REALM="${KEYCLOAK_REALM:-greenfield}"
ADMIN_USER="${KEYCLOAK_ADMIN:-admin}"
ADMIN_PASS="${KEYCLOAK_ADMIN_PASSWORD:-admin}"
OUT="${1:-deploy/compose/keycloak/realm.json}"

curl -sf -o /dev/null "$KC/realms/$REALM" || {
  echo "realm に到達できない: $KC/realms/$REALM" >&2; exit 1; }

TOKEN=$(curl -sf -X POST "$KC/realms/master/protocol/openid-connect/token" \
  -d client_id=admin-cli -d "username=$ADMIN_USER" -d "password=$ADMIN_PASS" -d grant_type=password \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')

TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
curl -sf -X POST "$KC/admin/realms/$REALM/partial-export?exportClients=true&exportGroupsAndRoles=true" \
  -H "Authorization: Bearer $TOKEN" -o "$TMP"

python3 - "$TMP" "$OUT" <<'PY'
import collections, json, sys

src, dst = sys.argv[1], sys.argv[2]
d = json.load(open(src), object_pairs_hook=collections.OrderedDict)

# partial-export はシークレットを伏せる。開発用の既定値を入れ直す。
# 本物の値をここへ書かないこと（この JSON は git に入る）。
SECRETS = {"ssr": "ssr-dev-secret", "svc-photo": "svc-photo-dev-secret", "svc-gear": "svc-gear-dev-secret"}
for c in d.get("clients", []):
    if c.get("clientId") in SECRETS:
        c["secret"] = SECRETS[c["clientId"]]

# service account ユーザは serviceAccountsEnabled から再生成されるので持たない
d["users"] = [u for u in d.get("users", []) if not u.get("username", "").startswith("service-account-")]

# 検証用ユーザ。org 属性が載ることの被験体（check #13）。
# partial-export は通常ユーザを返さないため、ここで固定して書き戻す。
d["users"].append(collections.OrderedDict([
    ("username", "alice"), ("enabled", True), ("emailVerified", True),
    ("email", "alice@example.test"), ("firstName", "Alice"), ("lastName", "Example"),
    ("attributes", {"org": ["rikuka"]}),
    ("credentials", [{"type": "password", "value": "alice-dev-password", "temporary": False}]),
    ("realmRoles", ["default-roles-greenfield"]),
]))

json.dump(d, open(dst, "w"), indent=2, ensure_ascii=False)
print(f"wrote {dst}")
PY

echo "git diff で意図した差分だけか確認すること"
