# ZAP baseline（passive）を夜間に回す（#189）

DAST はここまでの検査と決定的に違い、**動いているアプリが要る**。zap-baseline は
攻撃を撃たず通過したレスポンスだけを見る passive 検査で、主対象は API でなく
frontend——SSR の返す HTML はブラウザが実行するので、ヘッダ類が実際に意味を持つ。

## 形

- 夜間（03:47 JST、workflow_dispatch 可）に CI でスタック（mysql + rustfs + allinone +
  SSR）を立て、**SSR(:3000) と photo external(:8080) の2つ**に baseline を当てる
- 既知の検出は `.zap/rules.tsv` に**理由コメントつきで IGNORE**。`-I` を付けずに回すので、
  リストに無い**新規 alert だけ**がジョブを落とす（ADR 0021。ZAP に期限の機構は無いので
  exp: はコメントで持ち、期限見直しは人力）
- HTML レポートは artifact（14日保持）
- スコープ: baseline の spider は対象ホスト:ポートに閉じるので、署名付き URL の先の
  ストレージ(:9000)へは飛ばない（実測でも :9000 への到達なし）。内部/admin リスナーには
  当てない——到達不能はネットワーク層の性質で、ルート表分離は既存テストの担当

## 初回実測（2026-09-30、ローカル）

**SSR**: 60 PASS / 7 種の WARN。うち1つはその場で直した:

- **Permissions-Policy 欠落 → security.ts にヘッダ追加**（camera/microphone/geolocation/
  payment を明示で閉じる）。これが今回の検査の実利
- 残り6種は理由つき IGNORE:
  - 静的アセット(/assets/*)と robots.txt 404 のヘッダ欠落（10021/10038/10063）——
    react-router-serve の**静的配信層は route middleware を通らない**という発見。
    HTML 側の実在は #186 の e2e が担保しており、恒久対応はプロキシ/CDN 層(#211 と同じ層)
  - CSP unsafe-inline（10055）= #186 の既知の妥協 / CSRF トークン無し（10202）=
    Lax + POST-only の設計 / Non-Storable（10049）= 認証つき応答は no-store が正しい側 /
    COEP（90004）= 画像の別 origin 配信と衝突
- **photo external**: 66 PASS / WARN は Non-Storable のみ。API 面は素で静かだった

rules.tsv 適用後の最終確認: web `FAIL 0 / WARN-NEW 0 / IGNORE 7`、photo `FAIL 0 /
WARN-NEW 0 / IGNORE 1`、どちらも exit 0。

## 学び

- ZAP の指摘で唯一手を動かしたのは Permissions-Policy 1行。**既製 DAST の投資対効果は
  #182/#186（自前テスト）より確かに低い**が、「静的配信層がヘッダ管轄外」は ZAP でしか
  気づけなかった。層の違う目がひとつある価値はある
- macOS の Docker からホストへは `host.docker.internal`（`--network host` は効かない）。
  CI(Linux) は `--network host` でよい
