#!/usr/bin/env bash
# デプロイの3段を1段ずつ実行する。**順序を守らせるのはこのスクリプトではなく
# 呼び出す側のジョブグラフ**（.github/workflows/deploy.yml の needs）である。
#
#   dev/scripts/deploy.sh <service> migrate      expand を適用する（デプロイ前ステップ）
#   dev/scripts/deploy.sh <service> release      新しいイメージを building して差し替える
#   dev/scripts/deploy.sh <service> healthcheck  /healthz が 200 を返すまで待つ
#
# 3段を1つのスクリプトにまとめて順番に呼ぶ形にはしない。まとめると「migrate が
# 落ちたのに続きが走らないこと」の保証がスクリプトの制御フロー（= 読む人の信頼）に
# なる。ジョブグラフに置けば、保証は CI の実行機構そのものになる（#24 の主張）。
set -euo pipefail

svc=${1:?usage: deploy.sh <service> <migrate|release|healthcheck>}
step=${2:?usage: deploy.sh <service> <migrate|release|healthcheck>}
root=$(cd "$(dirname "$0")/../.." && pwd)
compose="$root/deploy/compose/compose.yaml"
dc() { docker compose -f "$compose" "$@"; }

# 各サービスの external ポート（compose の ports と対。ホスト側は 1xxxx へずらす）
case "$svc" in
  photo) host_port=18080 ;;
  gear)  host_port=18090 ;;
  *) echo "deploy.sh: 未知のサービス $svc（compose にデプロイ先を足してからここに追記する）" >&2; exit 2 ;;
esac

case "$step" in
migrate)
  # migrate は **アプリと同じイメージ**の別プロセスとして流す。デプロイ前ステップなので
  # サービスは古いコードのまま動いている。失敗したらここで止まり、release は実行されない。
  #
  # 資格情報は migrate 専用ユーザー（自database内の DDL のみ）。アプリの app ユーザーには
  # DDL を与えない（internal-05）。
  echo "== $svc: migrate expand"
  dc build "$svc"
  # migrate ユーザーの DSN はサービスごと(自 database 内の DDL のみ)。
  # 環境変数名も <SVC>_MIGRATE_DSN で揃っている
  up=$(echo "$svc" | tr '"'"'[:lower:]'"'"' '"'"'[:upper:]'"'"')
  dc run --rm --no-deps \
    -e "${up}_MIGRATE_DSN=${svc}_migrate:${svc}_migrate@tcp(mysql:3306)/${svc}" \
    "$svc" migrate expand
  ;;
release)
  echo "== $svc: release（イメージを差し替えて起動）"
  dc --profile deploy up -d --build --wait --wait-timeout 120 "$svc"
  ;;
healthcheck)
  echo "== $svc: healthcheck"
  for i in $(seq 1 40); do
    if curl -fsS "http://127.0.0.1:${host_port}/healthz" >/dev/null 2>&1; then
      echo "  /healthz 200（$i 回目）"
      # 何を出したかを残す。デプロイの成否と一緒に版が読めないと後から追えない
      curl -fsS "http://127.0.0.1:${host_port}/healthz"; echo
      exit 0
    fi
    sleep 1
  done
  echo "healthcheck: /healthz が 200 を返さない" >&2
  dc logs --tail 40 "$svc" >&2 || true
  exit 1
  ;;
*)
  echo "deploy.sh: 未知のステップ $step" >&2; exit 2 ;;
esac
