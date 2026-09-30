# npm サプライチェーン: ビルドスクリプトの許可リスト化（#184）

`postinstall` は `pnpm install` した瞬間に第三者のコードがビルドマシンと CI で走る経路で、
依存の CVE 検査（#183）では**一切見つからない**——CVE が付く前に走るし、そもそも
「脆弱性」ではなく意図された悪性コードだから。ADR 0016 の許可リスト主義をここにも敷く。

## 決めたこと（frontend/pnpm-workspace.yaml）

- **`allowBuilds: {}`** — 依存の install/postinstall スクリプトは列挙したものしか走らない。
  pnpm v10+ は既定で全拒否だが、**空を明示する**（「未設定だから拒否」でなく「拒否を選んだ」）。
  pnpm 12 では設定名が `onlyBuiltDependencies` → `allowBuilds` に変わっている（両方通るが
  ツール自身のヒントが出す現行名に合わせた）
- **`minimumReleaseAge: 4320`** — 公開から3日未満のバージョンを掴まない。
  乗っ取り→即公開→即取り込みの窓を閉じる。#181 の issue で「Renovate 側で検討」と
  していた項目は、pnpm 12 がネイティブに持っていたのでパッケージマネージャ側で解決

## 実測（2026-09-30）

- 現状の依存でビルドスクリプトを必要とするものは **0 件**。全 253 パッケージの
  package.json を走査した結果、install/preinstall/postinstall は 0、`prepare` のみが
  8 件（colorette 等。registry 取得では実行されない）
- インストールは供給網ポリシー検査つきで green:
  `Lockfile passes supply-chain policies (253 entries)`
- **負のテスト**: postinstall を持つ esbuild@0.25.0 を使い捨てワークスペースに追加して
  同じ設定で install →
  - スクリプトは実行されず `ERR_PNPM_IGNORED_BUILDS: Ignored build scripts: esbuild` で
    **install 自体が exit 1**。つまり新しいスクリプト付き依存は CI が構造的に落ち、
    `allowBuilds` への追記（= PR diff = レビュー）なしには入らない
  - `allowBuilds` キーが**無い**場合は警告どまりで install は通る——空でも明示することが
    ゲートの強度を変える（狙いどおり）

## 残り

- 許可リストに最初のエントリが入る日が来たら、行コメントに理由を書く（ADR 0021 の書式）
