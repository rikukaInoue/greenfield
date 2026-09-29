#!/usr/bin/env bash
# サービス間結合（ステージ 4.1）の一気通貫を確かめる:
# 生成クライアント（photo-client）→ アダプタ（photocatalog）→ interface 受け（usecase）の
# 経路が、実 Keycloak の M2M（svc-gear + scope internal:photo）で photo internal に届くこと。
#
#   dev/scripts/s2s-check.sh
#
# 前提: keycloak(8180) / mysql(13306) / photo(internal) / gear(external) が起動済み。
#       CI では authz ジョブが authn-check.sh の後に呼ぶ。
#
# 確かめること:
#   - gear external はトークン無しで 401、壊れたトークンで 401
#   - alice が機材を作れる（201）
#   - 詳細が 200 で photos キーを持つ（M2M の往復が成立。失敗なら 502 になる設計）
#   - photo 側に公開作例を用意すると詳細に**非空で**併合される
#     （「空の photos」と「取得成功」を区別できる形で確かめる）
#   - photo 側の非公開（private / pending_upload）の行は出ない
set -eu

KC="${KEYCLOAK_URL:-http://localhost:8180}/realms/greenfield/protocol/openid-connect/token"
GEAR_EXT="${GEAR_EXTERNAL_URL:-http://localhost:8090}"
MYSQL="${MYSQL_CMD:-mysql -h 127.0.0.1 -P 13306 -uroot -proot}"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

token() {
  curl -sf -X POST "$KC" "$@" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))'
}

curl -sf -o /dev/null "$GEAR_EXT/healthz" || { echo "到達できない: $GEAR_EXT" >&2; exit 1; }
echo "gear(ext): $GEAR_EXT"

ALICE=$(token -d client_id=agent -d grant_type=password -d username=alice -d password=alice-dev-password -d scope=openid)
[ -n "$ALICE" ] || { echo "alice のトークンが取れない" >&2; exit 1; }

echo
echo "1. gear external の認証（oidcauthn）"
C=$(curl -s -o /dev/null -w "%{http_code}" "$GEAR_EXT/items")
[ "$C" = 401 ] && ok "トークン無しは 401" || ng "無トークン: $C"
C=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer invalid.token.here" "$GEAR_EXT/items")
[ "$C" = 401 ] && ok "壊れたトークンは 401" || ng "壊れたトークン: $C"

echo
echo "2. 機材の投稿と詳細（M2M で photo internal を引く）"
BODY=$(curl -sf -X POST "$GEAR_EXT/items" -H "Authorization: Bearer $ALICE" -H "Content-Type: application/json" \
  -d '{"kind":"camera","name":"s2s-check","maker":"checker"}')
ID=$(echo "$BODY" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
[ -n "$ID" ] && ok "投稿できた（id=${ID}）" || ng "投稿に失敗: $BODY"

# 200 は M2M の往復の成立を意味する。photo internal に届かなければ設計上 502 になる
DETAIL=$(curl -s -w "\n%{http_code}" "$GEAR_EXT/items/$ID" -H "Authorization: Bearer $ALICE")
C=$(echo "$DETAIL" | tail -1)
if [ "$C" = 200 ]; then
  ok "詳細が 200（gear→photo の M2M が成立）"
else
  ng "詳細: $C: $(echo "$DETAIL" | head -1 | head -c 200)"
fi
echo "$DETAIL" | head -1 | python3 -c 'import sys,json;d=json.load(sys.stdin);assert isinstance(d.get("photos"),list),d' \
  && ok "photos キーが配列で存在" || ng "photos キーが無い"

echo
echo "3. 公開作例の併合（空と成功を区別する）"
# rustfs 無しでも確かめられるよう、公開済みの行を photo の DB に直接用意する。
# private / pending_upload の行は**出ないこと**まで見る
${MYSQL} photo <<SQL
INSERT INTO photos (owner_subject, visibility, gear_item_id, created_at, updated_at, object_key, content_type, size_bytes, status, title)
VALUES ('s2s-check', 'public',  ${ID}, NOW(6), NOW(6), '', 'image/png', 0, 'ready',          's2s-public'),
       ('s2s-check', 'private', ${ID}, NOW(6), NOW(6), '', 'image/png', 0, 'ready',          's2s-private'),
       ('s2s-check', 'public',  ${ID}, NOW(6), NOW(6), '', 'image/png', 0, 'pending_upload', 's2s-pending');
SQL
PHOTOS=$(curl -sf "$GEAR_EXT/items/$ID" -H "Authorization: Bearer $ALICE" \
  | python3 -c 'import sys,json;print("\n".join(p["caption"] for p in json.load(sys.stdin)["photos"]))')
echo "$PHOTOS" | grep -qx 's2s-public' && ok "公開作例が詳細に併合される（caption=s2s-public）" || ng "公開作例が出ない: [$PHOTOS]"
echo "$PHOTOS" | grep -q 's2s-private' && ng "非公開の作例が出ている" || ok "private は出ない"
echo "$PHOTOS" | grep -q 's2s-pending' && ng "アップロード未完了の作例が出ている" || ok "pending_upload は出ない"
${MYSQL} photo -e "DELETE FROM photos WHERE owner_subject='s2s-check';"

echo
echo "4. 一覧"
C=$(curl -s -o /tmp/s2s_body -w "%{http_code}" "$GEAR_EXT/items" -H "Authorization: Bearer $ALICE")
[ "$C" = 200 ] && grep -q '"s2s-check"' /tmp/s2s_body && ok "一覧に投稿が載る" || ng "一覧: $C"

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; fi
exit "$fail"
