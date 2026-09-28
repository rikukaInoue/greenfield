#!/usr/bin/env bash
# authz サービスが仕様どおりに判定するかを実 OpenFGA に対して確かめる（check #7 本番版）。
#
#   mise run authz:check
#
# 確かめること:
#   - 所有者は自分の photo を見られ、他人のは見られない（check / list-objects）
#   - platform operator は parent 経由で全 photo を見られる（オペレータ権限を ReBAC に載せる検証）
#   - 書き込み直後に consistency=higher の list-objects で可視（#7 の「作成直後可視」）
#   - 同一タプルの重複適用が無害（自然冪等）
#   - batch-check が順序を保存する
#
# タプルは毎回ユニークな ID 空間（実行時刻ベース）を使うので、再実行しても衝突しない。
set -eu

AUTHZ="${AUTHZ_URL:-http://localhost:8100}"
NS="${NS:-$(date +%s)-$$}"  # このランの photo ID 空間（秒 + PID で連続実行でも衝突しない）

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

post() { curl -sf -X POST "$AUTHZ$1" -H "Content-Type: application/json" -d "$2"; }
jsonget() { python3 -c "import sys,json;d=json.load(sys.stdin);print(json.dumps(d$1, ensure_ascii=False))"; }

if ! curl -sf -o /dev/null "$AUTHZ/healthz"; then
  echo "authz サービスに到達できない: ${AUTHZ}（mise run authz:run で起動する）" >&2
  exit 1
fi
echo "authz: $AUTHZ / photo ID 空間: p${NS}-*"

echo
echo "1. タプルを書く（alice が p1 の owner、bob が p2 の owner、carol が platform operator）"
W=$(post /tuples:write "{\"writes\":[
  {\"subject\":\"user:alice\",\"relation\":\"owner\",\"object\":\"photo:p${NS}-1\"},
  {\"subject\":\"user:bob\",\"relation\":\"owner\",\"object\":\"photo:p${NS}-2\"},
  {\"subject\":\"platform:main\",\"relation\":\"parent\",\"object\":\"photo:p${NS}-1\"},
  {\"subject\":\"platform:main\",\"relation\":\"parent\",\"object\":\"photo:p${NS}-2\"},
  {\"subject\":\"user:carol\",\"relation\":\"operator\",\"object\":\"platform:main\"}
]}")
# written + skipped = 5 を見る。冪等 API なので「新規に何件書いたか」ではなく
# 「呼び出し後に5タプルが在る状態になったか」が正しい合否（再実行でも成り立つ）。
WROTE=$(echo "$W" | jsonget "['written']"); WSKIP=$(echo "$W" | jsonget "['skipped']")
[ $(( ${WROTE:-0} + ${WSKIP:-0} )) = 5 ] && ok "5 タプルが在る状態（written=$WROTE skipped=${WSKIP}）" || ng "write: $W"

echo
echo "2. check: 所有と越境"
A=$(post /check "{\"subject\":\"user:alice\",\"action\":\"photo.view\",\"resource_type\":\"photo\",\"resource_id\":\"p${NS}-1\"}")
[ "$(echo "$A" | jsonget "['allowed']")" = true ] && ok "alice は自分の photo を見られる" || ng "alice/own: $A"
B=$(post /check "{\"subject\":\"user:alice\",\"action\":\"photo.view\",\"resource_type\":\"photo\",\"resource_id\":\"p${NS}-2\"}")
[ "$(echo "$B" | jsonget "['allowed']")" = false ] && ok "alice は bob の photo を見られない" || ng "alice/other: $B"
C=$(post /check "{\"subject\":\"user:carol\",\"action\":\"photo.view\",\"resource_type\":\"photo\",\"resource_id\":\"p${NS}-2\"}")
[ "$(echo "$C" | jsonget "['allowed']")" = true ] && ok "operator は parent 経由で他人の photo を見られる" || ng "operator: $C"
D=$(post /check "{\"subject\":\"user:carol\",\"action\":\"platform.operate\",\"resource_type\":\"platform\",\"resource_id\":\"main\"}")
[ "$(echo "$D" | jsonget "['allowed']")" = true ] && ok "platform.operate → operator が引ける" || ng "operate: $D"

echo
echo "3. list-objects: 認可付き一覧（WHERE IN の入力）"
L=$(post /list-objects "{\"subject\":\"user:alice\",\"action\":\"photo.view\",\"resource_type\":\"photo\"}")
LIDS=$(echo "$L" | python3 -c "import sys,json;ids=[i for i in json.load(sys.stdin)['object_ids'] if i.startswith('p${NS}-')];print(','.join(sorted(ids)))")
[ "$LIDS" = "p${NS}-1" ] && ok "alice の一覧は自分の分だけ（${LIDS}）" || ng "list alice: $LIDS"
LC=$(post /list-objects "{\"subject\":\"user:carol\",\"action\":\"photo.view\",\"resource_type\":\"photo\"}")
LCN=$(echo "$LC" | python3 -c "import sys,json;print(len([i for i in json.load(sys.stdin)['object_ids'] if i.startswith('p${NS}-')]))")
[ "$LCN" = 2 ] && ok "operator の一覧は全件（2件）" || ng "list carol: $LCN 件"

echo
echo "4. 作成直後の可視性（consistency=higher）— #7 の本丸"
post /tuples:write "{\"writes\":[{\"subject\":\"user:alice\",\"relation\":\"owner\",\"object\":\"photo:p${NS}-3\"}]}" > /dev/null
H=$(post /list-objects "{\"subject\":\"user:alice\",\"action\":\"photo.view\",\"resource_type\":\"photo\",\"consistency\":\"higher\"}")
case "$(echo "$H" | jsonget "['object_ids']")" in
  *"p${NS}-3"*) ok "書き込み直後に higher で可視" ;;
  *) ng "直後の higher で見えない: $H" ;;
esac

echo
echo "5. 自然冪等（同一タプルの重複適用）"
I=$(post /tuples:write "{\"writes\":[{\"subject\":\"user:alice\",\"relation\":\"owner\",\"object\":\"photo:p${NS}-1\"}]}")
[ "$(echo "$I" | jsonget "['written']")" = 0 ] && [ "$(echo "$I" | jsonget "['skipped']")" = 1 ] \
  && ok "重複 write は skipped=1 / written=0" || ng "冪等 write: $I"
J=$(post /tuples:write "{\"deletes\":[{\"subject\":\"user:zed\",\"relation\":\"owner\",\"object\":\"photo:p${NS}-none\"}]}")
[ "$(echo "$J" | jsonget "['deleted']")" = 0 ] && ok "不存在 delete も無害" || ng "冪等 delete: $J"

echo
echo "6. batch-check の順序保存"
BC=$(post /batch-check "{\"checks\":[
  {\"subject\":\"user:alice\",\"action\":\"photo.view\",\"resource_type\":\"photo\",\"resource_id\":\"p${NS}-2\"},
  {\"subject\":\"user:alice\",\"action\":\"photo.view\",\"resource_type\":\"photo\",\"resource_id\":\"p${NS}-1\"}
]}")
SEQ=$(echo "$BC" | python3 -c "import sys,json;print(','.join(str(r['allowed']).lower() for r in json.load(sys.stdin)['results']))")
[ "$SEQ" = "false,true" ] && ok "順序どおり（false,true）" || ng "batch: $SEQ"

echo
if [ "$fail" = 0 ]; then echo "すべて期待どおり"; else echo "期待と違う結果がある" >&2; fi
exit "$fail"
