# frontend の依存検査: osv-scanner + pnpm audit + Node ランタイム（#183）

SSR は Node がサーバとして本番に立つので、Go 側（govulncheck #180）と同じだけ依存の
面倒を見る。npm には到達性解析が無いので**件数が出る前提**で、差分運用（ADR 0021 /
`baseline-diff.sh`）に最初から乗せた——#191 で「2本目の検査の時点で仕組みを作る」と
決めたとおりの初適用。

## 形

- `mise run web:vuln`（`dev/scripts/frontend-deps-check.sh`）
  1. **osv-scanner** で `pnpm-lock.yaml` 全体（開発依存含む）。走査パッケージ数を出力し、
     0 なら空振りとして失敗
  2. **pnpm audit --prod をゲート**に。本番に載るのは prod だけで、`--prod` 無しだと
     vite / playwright / tailwind の CVE でノイズが出る。全体は参考情報として件数のみ表示
  3. どちらの検出も `dev/baselines/{osv-frontend,pnpm-audit-prod}.txt` との差分だけ失敗
     （除外は理由 + exp: 期限 + issue 必須、機構が検品）
- CI: PR ゲートは ci.yml の frontend ジョブ、**日次再検査は security.yml**
  （依存が1バイトも変わらなくても脆弱性 DB は毎日増える）
- lockfile: `pnpm install --frozen-lockfile`（web:install）は導入済みを確認
- **Node ランタイム自体の CVE**: 依存を上げても消えない（Go toolchain と同型の問題。
  Go 側は govulncheck が報告するが npm 側に相当品が無い）。mise のピンを endoflife.date の
  同メジャー最新パッチと日次で照合し、遅れたら落とす（`node-version-check.sh`。
  外部 API 依存なので PR ゲートには入れない）

## 実測（2026-09-30）

- osv-scanner: **268 パッケージ走査、検出 0**
- pnpm audit: `--prod` 0 件 / 全体も 0 件（Dependabot #181 導入直後で依存が新しい。
  「--prod と全体の差」の線引きは、差が出た時点で baseline に理由付きで刻まれる）
- node: ピン 24.21.0 = 24 系最新パッチ（一致）
- **負のテスト**: 既知脆弱の lodash 4.17.15 だけの使い捨て lockfile に同じパイプを通し、
  GHSA 6件が「新規」として exit 1 になることを確認——0件の緑が「検査が動いて0件」で
  あることの裏取り

## 落ち穂

- endoflife.date の API は `/api/node.json` でなく `/api/nodejs.json`（301 が JSON で
  ないため空振りガードが先に落ちて気づけた）
- `python3 -` にヒアドキュメントでプログラムを渡すとパイプの stdin と衝突する。
  データは引数のファイルで渡す
