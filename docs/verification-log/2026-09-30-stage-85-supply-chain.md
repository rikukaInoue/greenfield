# 2026-09-30 — [8.4] 配る側のサプライチェーン: 署名・SBOM・provenance（#218）

環境: GitHub Actions（image-release.yml）+ ghcr。AWS 非依存・費用ゼロ。
sec レーン（検出系5層、#194 完了）と対になる「証明の付与」側。

## 形

- **鍵を持たない署名**: cosign keyless（GH OIDC → Fulcio）。漏れる鍵が存在しない
- **SBOM**: syft の SPDX を attestation としてイメージに紐付け（空の SBOM は fail closed で拒否）
- **provenance**: GitHub の build attestation（どの workflow がどの commit から作ったか）
- **検証ゲート**: `dev/scripts/image-verify.sh` が3点を fail closed に確認。
  「push できた」は「うちが作った」を含意しない——同一性は certificate identity
  （image-release.yml の workflow ref）で縛る

## 実測

photo / gear / authz の3イメージで release → 検証:

| 確認 | 結果 |
|---|---|
| cosign verify（workflow identity 縛り） | ok（透明性ログ込み） |
| SBOM attestation（--type spdxjson） | ok |
| gh attestation verify（provenance） | ok |
| **偽の identity（deploy.yml を名乗る）での verify** | **拒否（exit 1）**——縛りが効いている実証 |

## 踏んだ罠（3連発。どれもドキュメントからは出てこない）

1. **GH Actions に `split()` は無い**。しかも式の構文エラーは push では通り、
   **workflow_dispatch の時点で初めて 422 になる**（検査の遅延）
2. **ghcr のリポジトリ名は小文字必須**。`github.repository` は大文字を含みうる
   （rikukaInoue）ので、シェルで `tr '[:upper:]' '[:lower:]'` を通す
3. **cosign v3 の verify-attestation は OCI referrers しか読まない**。既定の
   `cosign attest` は legacy の .att タグに付けるため、「attest は成功したのに
   検証側からは SBOM が無いように見える」（cosign tree では .att に存在）。
   `--new-bundle-format=true` で referrers に付けて解消。**付与の成功は検証可能性を
   含意しない**——fail open の親戚がここにも居た
