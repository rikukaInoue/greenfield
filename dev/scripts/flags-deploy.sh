#!/usr/bin/env bash
# フラグ専用パイプライン(docs/adr/0011、7.5/#79)。
#
# git 管理のフラグ定義(flagd 形式が正)を AWS AppConfig の feature flags 形式へ
# 変換し、新しい構成バージョンとして登録してデプロイする。
# アプリのデプロイとは独立: フラグの切替・巻き戻しにアプリのデプロイは要らない。
#
# 使い方:
#   APPCONFIG_APPLICATION=<id> APPCONFIG_ENVIRONMENT=<id> APPCONFIG_PROFILE=<id> \
#   APPCONFIG_STRATEGY=<id> dev/scripts/flags-deploy.sh [flags.json]
#
# 引数を省略すると deploy/compose/flagd/flags.json(ローカル flagd と同じ定義)を使う。
# デプロイの完了(ベイク含む)まで待ち、最終状態が COMPLETE でなければ失敗する。
set -euo pipefail

SRC="${1:-deploy/compose/flagd/flags.json}"
: "${APPCONFIG_APPLICATION:?}" "${APPCONFIG_ENVIRONMENT:?}" "${APPCONFIG_PROFILE:?}" "${APPCONFIG_STRATEGY:?}"

[ -f "$SRC" ] || { echo "NG: 定義ファイルが無い: $SRC" >&2; exit 1; }

# flagd 形式 → AppConfig feature flags 形式(hosted configuration の入力)。
# boolean 以外の variants は今の規約に無いので、見つけたら変換を失敗させる
# (黙って落とすと定義の欠けが「デプロイ成功」として出てくる)。
#
# AppConfig のフラグキーは ^[a-z][a-zA-Z0-9_-]{0,63}$ で **"." を含められない**(7.5 実測)。
# 命名規約(<種類>_<機能名>、internal-08)はこの制約に収まる。外れた名前はここで止める。
CONTENT=$(python3 - "$SRC" <<'PY'
import json, re, sys
src = json.load(open(sys.argv[1]))
flags, values = {}, {}
for key, body in src.get("flags", {}).items():
    variants = body.get("variants", {})
    default = body.get("defaultVariant")
    if default not in variants or not isinstance(variants[default], bool):
        sys.exit(f"NG: {key} は boolean フラグでない(variants={variants}, default={default})")
    if not re.fullmatch(r"[a-z][a-zA-Z0-9_-]{0,63}", key):
        sys.exit(f"NG: {key} は AppConfig のキー制約(かつ命名規約)に合わない")
    flags[key] = {"name": key}
    values[key] = {"enabled": variants[default]}
if not flags:
    sys.exit("NG: フラグが1つも無い(空の定義を配らない)")
print(json.dumps({"flags": flags, "values": values, "version": "1"}))
PY
)

echo "反映する定義: $CONTENT"

TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
printf '%s' "$CONTENT" > "$TMP"
VERSION=$(aws appconfig create-hosted-configuration-version \
  --application-id "$APPCONFIG_APPLICATION" \
  --configuration-profile-id "$APPCONFIG_PROFILE" \
  --content "fileb://$TMP" \
  --content-type application/json \
  /dev/null --query VersionNumber --output text)
echo "構成バージョン: $VERSION"

DEPLOY=$(aws appconfig start-deployment \
  --application-id "$APPCONFIG_APPLICATION" \
  --environment-id "$APPCONFIG_ENVIRONMENT" \
  --configuration-profile-id "$APPCONFIG_PROFILE" \
  --configuration-version "$VERSION" \
  --deployment-strategy-id "$APPCONFIG_STRATEGY" \
  --query DeploymentNumber --output text)
echo "デプロイ #$DEPLOY 開始"

for _ in $(seq 1 120); do
  STATE=$(aws appconfig get-deployment \
    --application-id "$APPCONFIG_APPLICATION" \
    --environment-id "$APPCONFIG_ENVIRONMENT" \
    --deployment-number "$DEPLOY" --query State --output text)
  case "$STATE" in
    COMPLETE) echo "ok: デプロイ #$DEPLOY COMPLETE(バージョン $VERSION)"; exit 0 ;;
    ROLLED_BACK|REVERTED) echo "NG: デプロイ #$DEPLOY が $STATE" >&2; exit 1 ;;
    *) sleep 5 ;;
  esac
done
echo "NG: デプロイ #$DEPLOY が時間内に完了しない(最終状態: $STATE)" >&2
exit 1
