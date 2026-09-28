#!/usr/bin/env bash
# api/ の OpenAPI JSON から人が読める HTML を生成する（#77）。
#
# huma の /docs と /openapi.json は意図的に切ってある（スペックはアプリから配らず、
# api/ を唯一の契約置き場にする。core/httpapi）。この方針は変えず、**生成物としての
# HTML** をここで作る。入力は api/ の生成物だけ。手書きのスペックを混ぜない。
#
# リスナーごとに別ページにする。api-design §3.4 が「external クライアントに admin の
# 型が混ざると管理APIの存在が漏れる」としてパッケージを分けるのと同じ形。
# なおこのリポジトリ自体は public なので、admin/internal の存在は api/ の JSON の時点で
# 公開されている。ページを分けるのは還流先（private な本番リポジトリ）での運用の雛形。
#
#   dev/scripts/api-docs.sh [出力dir]   # 既定 api-docs/
set -euo pipefail
cd "$(dirname "$0")/../.."

out=${1:-api-docs}
rm -rf "$out" && mkdir -p "$out"

# Redocly CLI は pnpm dlx で固定バージョンを都度取得する。frontend の package.json に
# 入れないのは、これがフロントの依存ではなくリポジトリの生成ツールだから
# （バージョンはここで固定し、更新はこのファイルの diff としてレビューに載せる）。
REDOCLY_PKG=@redocly/cli@2.4.0

rows=""
for spec in api/*/*.openapi.json; do
  svc=$(basename "$(dirname "$spec")")
  # external.v2.openapi.json → external.v2
  name=$(basename "$spec" .openapi.json)
  html="$out/$svc/$name.html"
  mkdir -p "$out/$svc"
  echo "== $spec -> $html"
  # dlx はパッケージ名とバイナリ名が違うと選べないので --package で分けて渡す
  pnpm --dir frontend --package="$REDOCLY_PKG" dlx redocly build-docs "$spec" --output "$html" > /dev/null
  title=$(python3 -c "import json,sys; i=json.load(open('$spec'))['info']; print(i['title'], i['version'])")
  rows="$rows<li><a href=\"$svc/$name.html\">$svc / $name</a> — $title</li>\n"
done

# 索引。生成物なので手で編集しない
cat > "$out/index.html" <<HTML
<!doctype html>
<meta charset="utf-8">
<title>greenfield API docs</title>
<style>body{font-family:sans-serif;max-width:48rem;margin:3rem auto;line-height:1.7}</style>
<h1>greenfield API docs</h1>
<p>生成元は <code>api/</code> の OpenAPI JSON（唯一の契約置き場）。このページも生成物。</p>
<p><strong>還流時の注意</strong>: private な本番リポジトリでは external だけを公開し、
admin / internal のページは公開先を分けること（api-design §3.4）。
このリポジトリは public なので分けても隠せず、雛形として全リスナーを載せている。</p>
<ul>
$(printf "%b" "$rows")
</ul>
HTML

echo "生成完了: $out/ ($(find "$out" -name "*.html" | wc -l | tr -d " ") ページ)"
