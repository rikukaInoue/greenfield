#!/usr/bin/env bash
# frontend 依存の既知脆弱性検査（#183）。SSR は Node がサーバとして本番に立つので、
# Go 側（govulncheck）と同じだけ依存の面倒を見る。
#
#   mise run web:vuln
#
# 構成:
#   1. osv-scanner で pnpm-lock.yaml を検査（開発依存も含む全体）
#   2. pnpm audit --prod をゲートに（本番に載るのは prod だけ。全体は参考情報）
# どちらも govulncheck と違って到達性を見ないので件数が出る前提で、
# 差分運用（ADR 0021 / baseline-diff.sh）に乗せる。除外には理由と期限が要る。
set -eu
cd "$(dirname "$0")/../.."

fail=0
ng() { echo "  NG: $*" >&2; fail=1; }
ok() { echo "  ok: $*"; }

echo "1. osv-scanner（pnpm-lock.yaml 全体）"
osv_json=$(mktemp); osv_err=$(mktemp)
trap 'rm -f "$osv_json" "$osv_err"' EXIT
# exit 0=検出なし / 1=検出あり はどちらも「検査は動いた」。それ以外は検査自体の失敗
set +e
osv-scanner scan --lockfile frontend/pnpm-lock.yaml --format json > "$osv_json" 2> "$osv_err"
osv_exit=$?
set -e
if [ "$osv_exit" -gt 1 ]; then
  echo "osv-scanner が異常終了（exit=$osv_exit）:" >&2
  tail -5 "$osv_err" >&2
  exit 1
fi
# 空振りガード: 走査したパッケージ数を必ず確かめる（lockfile を見失って0件緑を防ぐ）
scanned=$(grep -oE 'found [0-9]+ packages' "$osv_err" | grep -oE '[0-9]+' | head -1)
if [ -z "${scanned:-}" ] || [ "$scanned" -eq 0 ]; then
  echo "osv-scanner が0パッケージしか走査していない。検査が空振りしている" >&2
  exit 1
fi
echo "  走査したパッケージ: $scanned"
python3 - "$osv_json" <<'PY' | dev/scripts/baseline-diff.sh dev/baselines/osv-frontend.txt "osv-scanner(frontend)" || fail=1
import json, sys
d = json.load(open(sys.argv[1]))
seen = set()
for r in d.get("results") or []:
    for p in r.get("packages") or []:
        name = p.get("package", {}).get("name", "?")
        for v in p.get("vulnerabilities") or []:
            seen.add(f'{v.get("id","?")} {name}')
print("\n".join(sorted(seen)))
PY

echo "2. pnpm audit --prod（ゲート。本番に載る依存だけ）"
prod_file=$(mktemp)
trap 'rm -f "$osv_json" "$osv_err" "$prod_file"' EXIT
# 検出ありだと exit 非0 を返すので || true。出力が JSON でなければ次の検品で落ちる
(cd frontend && pnpm audit --prod --json > "$prod_file" 2>/dev/null) || true
if ! python3 -c "import json,sys;json.load(open(sys.argv[1]))" "$prod_file" 2>/dev/null; then
  echo "pnpm audit --prod の出力が JSON として読めない。検査が空振りしている:" >&2
  head -3 "$prod_file" >&2
  exit 1
fi
python3 - "$prod_file" <<'PY' | dev/scripts/baseline-diff.sh dev/baselines/pnpm-audit-prod.txt "pnpm-audit(prod)" || fail=1
import json, sys
d = json.load(open(sys.argv[1]))
advisories = d.get("advisories") or {}
lines = set()
for a in advisories.values():
    ident = a.get("github_advisory_id")
    if not ident:
        cves = a.get("cves") or []
        ident = cves[0] if cves else str(a.get("id", "?"))
    lines.add(f'{ident} {a.get("module_name","?")}')
print("\n".join(sorted(str(l) for l in lines)))
PY

echo "3. pnpm audit 全体（参考情報。ゲートではない）"
cd frontend
all_counts=$(pnpm audit --json 2>/dev/null | python3 -c 'import sys,json;print(json.load(sys.stdin).get("metadata",{}).get("vulnerabilities",{}))' || echo "取得失敗")
cd ..
echo "  開発依存を含む全体: $all_counts"

exit $fail
