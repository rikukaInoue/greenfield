#!/usr/bin/env bash
# 配る側の証明の検証(#218)。デプロイ前ゲートとして使う:
#   1. cosign 署名 — この repo の image-release ワークフローが署名したか
#   2. SBOM attestation — SPDX が紐付いているか
#   3. GitHub provenance — どの commit からビルドされたか
# どれか1つでも欠けたら失敗(fail closed)。「push できた」は「うちが作った」を含意しない。
set -euo pipefail
IMAGE=${1:?usage: image-verify.sh <image@digest>}

IDENTITY_RE="^https://github.com/rikukaInoue/greenfield/\.github/workflows/image-release\.yml@"
ISSUER="https://token.actions.githubusercontent.com"

echo "== 1. 署名(cosign keyless)"
cosign verify \
  --certificate-identity-regexp "$IDENTITY_RE" \
  --certificate-oidc-issuer "$ISSUER" \
  "$IMAGE" > /dev/null
echo "   ok: image-release ワークフローの署名を確認"

echo "== 2. SBOM attestation(SPDX)"
cosign verify-attestation --type spdxjson \
  --certificate-identity-regexp "$IDENTITY_RE" \
  --certificate-oidc-issuer "$ISSUER" \
  "$IMAGE" > /dev/null
echo "   ok: SBOM が紐付いている"

echo "== 3. ビルド provenance(GitHub attestation)"
gh attestation verify "oci://$IMAGE" --repo rikukaInoue/greenfield > /dev/null
echo "   ok: provenance を確認"

echo "検証OK: $IMAGE"
