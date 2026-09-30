# [8.4] 追補: 無公開で回すサプライチェーン証明の変種（#218 / gap-9）

**統合済みの注記**: #218 は並行して2実装が生まれ、本線は
[stage-85](2026-09-30-stage-85-supply-chain.md) の `image-release.yml`
（ghcr へ公開 + レジストリ添付の attestation + `image-verify.sh` の fail closed 検証 +
偽 identity の拒否実証）に一本化した。このログは撤去した週次ワークフローの記録として
残す——固有の価値は「**公開せずに** tar へ署名・provenance を付けて検証まで通す」変種
（レジストリ不要で PR 上でも証明チェーンを検証できる）と、公開を承認制の入力に
分離した運用ノートにある。

検出系（#194 の5層）が「入ってくるもの」を見るのに対し、こちらは「出ていくもの」に
証明を付ける——このイメージは何からできていて（SBOM）、どこで誰がビルドし
（provenance）、改竄されていないか（署名）。`.github/workflows/supply-chain.yml`
（週次 + 手動）で photo / gear / authz の3イメージに対して行う。

## 形

1. ビルド → `docker save` で tar 化
2. **SBOM**（syft、SPDX JSON）+ 空振りガード（パッケージ数 <10 で失敗）
3. **provenance**（`actions/attest-build-provenance`。GitHub の attestation ストアへ）
4. **署名**（cosign keyless。鍵を保管せず、GitHub OIDC の実行時証明書で署名）
5. **その場で検証** — これが適応度関数:
   - `cosign verify-blob --certificate-identity-regexp`（**このリポジトリのこのワークフロー
     由来**であることの検証。「どの鍵か」でなく「どこでビルドされたか」を確かめる）
   - `gh attestation verify`
   - 通らなければジョブが落ちる。「署名したつもり」と「検証可能」を区別する
6. SBOM と署名バンドルは artifact（30日）

**公開は承認制**: GHCR への push とレジストリ上のイメージ署名は `publish` 入力
（既定 false）の後ろに置いた。事前承認規約の「外部に出るものは毎回確認」に従う。
既定実行で外に出るのは Rekor 透明性ログの署名記録と repo の attestation ストアの
メタデータ（どちらもハッシュと証明書情報のみ）。

## 実測（2026-09-30、CI 上）

- 3イメージすべて green。SBOM パッケージ数: photo **113** / gear **85** / authz 同等
  （Go バイナリ + alpine 3.22 の OS パッケージ）
- 署名検証・provenance 検証とも「その場」で成立
- 初回2回の転び: ① GHCR のイメージ名は小文字必須（`GITHUB_REPOSITORY` の owner に
  大文字が入る → `${GITHUB_REPOSITORY,,}`）② tag の sha 短縮とフル sha の混在

## 使いどころ（何のためにあるか）

- **CVE が出た日**: レジストリを再スキャンせず、SBOM に問い合わせるだけで
  「うちのどのイメージに入っているか」が答えられる（Trivy の日次再スキャン #187 と相補）
- **デプロイ時**: 本番 pull の前に `cosign verify` + `gh attestation verify` を挟めば、
  「この CI を通っていないイメージは動かない」を機械で言える（配線は本番スタックが
  できる時点で。検証コマンドはワークフローに実物がある）
