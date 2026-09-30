#!/usr/bin/env bash
# Trivy による設定・シークレットの検査（#187）。依存 CVE は #180(govulncheck) /
# #183(osv-scanner) の担当なので、ここではスキャナを設定とシークレットに絞る。
#
#   mise run trivy:check            # 設定 + シークレット（PR ゲート）
#   mise run trivy:check -- image   # 上に加えてイメージ3本をビルドして検査（日次）
#
# 除外は .trivyignore.yaml（理由 statement + 期限 expired_at 必須。期限切れは trivy が
# 自動で除外を無効化するのでゲートが再び落ちる = ADR 0021 の差分運用そのもの）。
set -eu
cd "$(dirname "$0")/../.."

MODE="${1:-}"

# --- .trivyignore.yaml の検品: 理由と期限の無い除外を受け付けない（ADR 0021） ---
python3 - <<'PY'
import sys, re
try:
    import yaml
    entries = []
    d = yaml.safe_load(open(".trivyignore.yaml")) or {}
    for section in d.values():
        entries += section or []
except ModuleNotFoundError:
    # yaml が無い環境では素朴に数える（id: の数 = statement: の数 = expired_at: の数）
    s = open(".trivyignore.yaml").read()
    ids, st, ex = (len(re.findall(rf"^\s*-?\s*{k}:", s, re.M)) for k in ("id", "statement", "expired_at"))
    if not (ids == st == ex):
        print(f".trivyignore.yaml: id {ids} 件に対し statement {st} / expired_at {ex}。理由か期限の無い除外がある", file=sys.stderr)
        sys.exit(1)
    print(f"  除外の検品: {ids} 件、全てに理由と期限あり")
    sys.exit(0)
bad = [e.get("id") for e in entries if not e.get("statement") or not e.get("expired_at")]
if bad:
    print(f".trivyignore.yaml: 理由(statement)か期限(expired_at)の無い除外: {bad}", file=sys.stderr)
    sys.exit(1)
print(f"  除外の検品: {len(entries)} 件、全てに理由と期限あり")
PY

# --- canary: シークレット検出器の生存確認（空振り防止） ---
# 「検出0件」を報告する前に、検出できる状態であることを毎回証明する
canary=$(mktemp -d)
trap 'rm -rf "$canary"' EXIT
printf 'aws_access_key_id = AKIAZZZZZZZZZZZZZZZZ\naws_secret_access_key = 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\n' > "$canary/creds.txt"
if trivy fs --scanners secret --exit-code 1 --quiet "$canary" >/dev/null 2>&1; then
  echo "シークレット検出器が canary を見逃した。検査が空振りしている" >&2
  exit 1
fi
echo "  ok: canary（ダミーのAWSキー）を検出できる状態"

echo "1. 設定（Dockerfile / compose / Terraform）"
trivy config --exit-code 1 --severity HIGH,CRITICAL --ignorefile .trivyignore.yaml --quiet .
echo "  ok: HIGH/CRITICAL の新規ミスコンフィグなし"

echo "2. シークレット（リポジトリ全体）"
trivy fs --scanners secret --exit-code 1 --ignorefile .trivyignore.yaml --quiet .
echo "  ok: ハードコードされた鍵なし"

if [ "$MODE" = "image" ]; then
  echo "3. イメージ（3本ビルドして OS パッケージの CVE。ignore-unfixed: 修正版の無い CVE で落としても打つ手が無い）"
  for svc in services/photo services/gear platform/authz; do
    name="trivy-scan-$(basename "$svc")"
    echo "  == $svc"
    docker build -q -f "$svc/Dockerfile" -t "$name" . >/dev/null
    trivy image --exit-code 1 --ignore-unfixed --severity HIGH,CRITICAL --quiet "$name"
  done
  echo "  ok: イメージに修正可能な HIGH/CRITICAL なし"
fi
