#!/usr/bin/env bash
# 同期コマンド + pending 状態 + 回収ジョブ（ステージ 4.4 / check #11）の一気通貫。
#
#   dev/scripts/pending-check.sh request   # gear 停止中: 紐付け要求 → pending で成立
#   dev/scripts/pending-check.sh settle    # gear 復旧後: 回収ジョブが冪等キー照会で確定
#
# 2フェーズに分かれているのは実験の形そのものが「gear 停止中 → 復旧後」だから。
# CI の authz ジョブでは photo 起動直後（gear 未起動）に request、gear 起動後に settle を呼ぶ。
#
# 確かめること:
#   - gear 停止中でも投稿は成立し、紐付けは pending に留まる（失敗にしない）
#   - 回収ジョブが冪等キーで照会・再送して linked / rejected に確定する
#   - 二重紐付けなし: 再回収は対象0、gear の受理記録は1キー1行のまま
set -eu

PHASE="${1:-}"
case "$PHASE" in request | settle) ;; *) echo "usage: pending-check.sh request|settle" >&2; exit 2 ;; esac

KC="${KEYCLOAK_URL:-http://localhost:8180}/realms/greenfield/protocol/openid-connect/token"
PHOTO_EXT="${PHOTO_EXTERNAL_URL:-http://localhost:8080}"
MYSQL="${MYSQL_CMD:-mysql -h 127.0.0.1 -P 13306 -uroot -proot}"
# 回収ジョブの実行コマンド（環境は呼び出し側が整える）
RECLAIM="${RECLAIM_CMD:-}"
STATE="${PENDING_CHECK_STATE:-/tmp/pending-check-state}"

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

token() {
  curl -sf -X POST "$KC" -d client_id=agent -d grant_type=password -d username=alice -d password=alice-dev-password \
    | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))'
}

create() { # create <gear_item_id> -> "id status"
  curl -sf -X POST "$PHOTO_EXT/v2/photos" -H "Authorization: Bearer $ALICE" -H "Content-Type: application/json" \
    -d "{\"caption\":\"pending-check\",\"content_type\":\"image/png\",\"gear_item_id\":$1}" \
    | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["id"], d.get("gear_link_status",""))'
}

ALICE=$(token)
[ -n "$ALICE" ] || { echo "alice のトークンが取れない" >&2; exit 1; }

if [ "$PHASE" = request ]; then
  echo "1. gear 停止中の紐付け要求（check #11 前半）"
  curl -sf -o /dev/null --max-time 2 "${GEAR_INTERNAL_URL:-http://localhost:18091}/healthz" \
    && ng "gear が動いている（この検査は停止中に走らせる）" \
    || ok "gear は停止している"

  read -r ID1 S1 <<<"$(create 1)"
  [ "$S1" = pending ] && ok "投稿は成立し pending（id=${ID1}）" || ng "id=${ID1} status=${S1}"
  read -r ID2 S2 <<<"$(create 99999)"
  [ "$S2" = pending ] && ok "存在しない機材でも投稿は成立し pending（結果は gear が決める）" || ng "id=${ID2} status=${S2}"

  KEY1=$(${MYSQL} -N photo -e "SELECT gear_link_key FROM photos WHERE id=${ID1};" 2>/dev/null)
  [ -n "$KEY1" ] && ok "冪等キーが採番されている" || ng "キーが無い"
  echo "${ID1} ${ID2} ${KEY1}" > "$STATE"
else
  [ -f "$STATE" ] || { echo "request フェーズの記録が無い（$STATE）" >&2; exit 1; }
  read -r ID1 ID2 KEY1 < "$STATE"
  [ -n "$RECLAIM" ] || { echo "RECLAIM_CMD が未設定" >&2; exit 1; }

  echo "2. 回収ジョブによる確定（check #11 後半）"
  S1=$(${MYSQL} -N photo -e "SELECT gear_link_status FROM photos WHERE id=${ID1};" 2>/dev/null)
  [ "$S1" = pending ] && ok "回収前は pending のまま" || ng "回収前: $S1"

  $RECLAIM || ng "回収ジョブが失敗"
  S1=$(${MYSQL} -N photo -e "SELECT gear_link_status FROM photos WHERE id=${ID1};" 2>/dev/null)
  S2=$(${MYSQL} -N photo -e "SELECT gear_link_status FROM photos WHERE id=${ID2};" 2>/dev/null)
  [ "$S1" = linked ] && ok "存在する機材は linked に確定" || ng "id=${ID1}: $S1"
  [ "$S2" = rejected ] && ok "存在しない機材は rejected に確定" || ng "id=${ID2}: $S2"

  G=$(${MYSQL} -N gear -e "SELECT status FROM photo_links WHERE link_key='${KEY1}';" 2>/dev/null)
  [ "$G" = linked ] && ok "gear 側の受理記録がキーで引ける" || ng "gear 記録: $G"

  echo
  echo "3. 二重紐付けなし"
  N_BEFORE=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM photo_links;" 2>/dev/null)
  $RECLAIM || ng "再回収が失敗"
  N_AFTER=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM photo_links;" 2>/dev/null)
  [ "$N_BEFORE" = "$N_AFTER" ] && ok "再回収しても受理記録は増えない（${N_AFTER}行のまま）" || ng "増えた: $N_BEFORE → $N_AFTER"
  DUP=$(${MYSQL} -N gear -e "SELECT COUNT(*) FROM photo_links WHERE link_key='${KEY1}';" 2>/dev/null)
  [ "$DUP" = 1 ] && ok "同じキーの記録は1行だけ" || ng "キー ${KEY1} が ${DUP} 行"
  rm -f "$STATE"
fi

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; fi
exit "$fail"
