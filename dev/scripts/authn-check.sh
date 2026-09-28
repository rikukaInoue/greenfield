#!/usr/bin/env bash
# 本番アダプタ（oidcauthn + authzhttp）へ差し替えた photo が、実 Keycloak の
# トークンで認証・認可されるかを一気通貫で確かめる（check #27 ほか、ステージ 3.3）。
#
#   mise run authn:check
#
# 前提: keycloak(8180) / openfga(8280) / mysql(13306) が起動済みで、
#       photo が OIDC_ISSUER 付き（=OIDCDeps）で、authz が OIDC_ISSUER 付きで動いていること。
#       mise run authn:check はこの起動まで面倒を見る。
#
# 確かめること:
#   - 外部APIはトークン無しで 401、alice のトークンで 200（authzhttp 経由の認可付き一覧）
#   - 内部APIはトークン無しで 401（#27）
#   - 内部APIは人間のトークン（internal:photo 無し）で 403（#27）
#   - 内部APIは svc-gear の M2M トークン（scope internal:photo）で 200
#   - authz サービス自身もトークン無しで 401（internal:platform で 200）
set -eu

KC="${KEYCLOAK_URL:-http://localhost:8180}/realms/greenfield/protocol/openid-connect/token"
PHOTO_EXT="${PHOTO_EXTERNAL_URL:-http://localhost:8080}"
PHOTO_INT="${PHOTO_INTERNAL_URL:-http://localhost:8081}"
AUTHZ="${AUTHZ_URL:-http://localhost:8100}"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

token() { # token <curl form args...>
  curl -sf -X POST "$KC" "$@" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))'
}
code() { # code <url> [token]
  if [ -n "${2:-}" ]; then
    curl -s -o /tmp/authn_body -w "%{http_code}" -H "Authorization: Bearer $2" "$1"
  else
    curl -s -o /tmp/authn_body -w "%{http_code}" "$1"
  fi
}

for url in "$PHOTO_EXT/healthz" "$PHOTO_INT/healthz" "$AUTHZ/healthz"; do
  curl -sf -o /dev/null "$url" || { echo "到達できない: $url" >&2; exit 1; }
done

echo "photo(ext): $PHOTO_EXT / photo(int): $PHOTO_INT / authz: $AUTHZ"

ALICE=$(token -d client_id=agent -d grant_type=password -d username=alice -d password=alice-dev-password -d scope=openid)
GEAR=$(token -d client_id=svc-gear -d client_secret=svc-gear-dev-secret -d grant_type=client_credentials -d scope=internal:photo)
GEAR_NOSCOPE=$(token -d client_id=svc-gear -d client_secret=svc-gear-dev-secret -d grant_type=client_credentials)
PLATFORM=$(token -d client_id=svc-photo -d client_secret=svc-photo-dev-secret -d grant_type=client_credentials -d scope=internal:platform)
[ -n "$ALICE" ] && [ -n "$GEAR" ] && [ -n "$PLATFORM" ] || { echo "トークンが取れない" >&2; exit 1; }

echo
echo "1. 外部API（oidcauthn + authzhttp の一気通貫）"
C=$(code "$PHOTO_EXT/v2/photos")
[ "$C" = 401 ] && ok "トークン無しは 401" || ng "無トークン: $C"
C=$(code "$PHOTO_EXT/v2/photos" "$ALICE")
[ "$C" = 200 ] && ok "alice のトークンで 200（認可付き一覧が authz サービス経由で返る）" || ng "alice: $C: $(head -c 200 /tmp/authn_body)"
C=$(code "$PHOTO_EXT/v2/photos" "invalid.token.here")
[ "$C" = 401 ] && ok "壊れたトークンは 401" || ng "壊れたトークン: $C"

echo
echo "2. 内部API（#27: 無認証で叩けない）"
C=$(code "$PHOTO_INT/gear-items/1/photos")
[ "$C" = 401 ] && ok "トークン無しは 401" || ng "無トークン: $C"
C=$(code "$PHOTO_INT/gear-items/1/photos" "$ALICE")
[ "$C" = 403 ] && ok "人間のトークン（internal:photo 無し）は 403" || ng "alice→internal: $C"
C=$(code "$PHOTO_INT/gear-items/1/photos" "$GEAR_NOSCOPE")
[ "$C" = 403 ] && ok "M2M でもスコープ無しは 403" || ng "gear(noscope): $C"
C=$(code "$PHOTO_INT/gear-items/1/photos" "$GEAR")
[ "$C" = 200 ] && ok "svc-gear + internal:photo で 200" || ng "gear: $C: $(head -c 200 /tmp/authn_body)"

echo
echo "3. authz サービス自身（無認証は採らない）"
C=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$AUTHZ/check" -H "Content-Type: application/json" -d '{}')
[ "$C" = 401 ] && ok "トークン無しは 401" || ng "authz 無トークン: $C"
C=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$AUTHZ/check" -H "Content-Type: application/json" \
     -H "Authorization: Bearer $ALICE" -d '{}')
[ "$C" = 403 ] && ok "internal:platform 無しは 403" || ng "authz alice: $C"
C=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$AUTHZ/check" -H "Content-Type: application/json" \
     -H "Authorization: Bearer $PLATFORM" \
     -d '{"subject":"user:probe","action":"photo.view","resource_type":"photo","resource_id":"1"}')
[ "$C" = 200 ] && ok "internal:platform で 200" || ng "authz platform: $C"

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; fi
exit "$fail"
