#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

for path in \
  go.mod \
  LICENSE \
  cosign.pub \
  README.md \
  manifest.json \
  config.schema.json \
  .github/workflows/oci-release.yml \
  release/test_oci_artifact_layout.sh \
  marketplace/manifest.yaml.tmpl \
  marketplace/README.md \
  marketplace/CHANGELOG.md \
  marketplace/icon.png; do
  [ -f "$ROOT/$path" ] || fail "missing public-plugin artifact: $path"
done

grep -Fqx 'module github.com/nextmoca/tyk-ai-studio-runtime-defense' "$ROOT/go.mod" \
  || fail 'Go module must identify the standalone public repository'

grep -Fq -- 'BEGIN PUBLIC KEY' "$ROOT/cosign.pub" \
  || fail 'Cosign public verification key is missing or malformed'

if [ -e "$ROOT/Dockerfile" ] || [ -e "$ROOT/.dockerignore" ]; then
  fail 'public Tyk plugin must publish a binary OCI artifact, not a container image'
fi

grep -Fq '"version": "0.1.3"' "$ROOT/manifest.json" \
  || fail 'release must identify as 0.1.3'

grep -Fq 'ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin' "$ROOT/.github/workflows/oci-release.yml" \
  || fail 'release workflow must publish the designated adapter artifact'

grep -Fq 'https://github.com/nextmoca/tyk-ai-studio-runtime-defense' "$ROOT/marketplace/manifest.yaml.tmpl" \
  || fail 'Marketplace metadata must link to the standalone public repository'

if [ "$(wc -c < "$ROOT/marketplace/icon.png")" -gt 102400 ]; then
  fail 'Marketplace icon must be at most 100 KiB'
fi

if rg -n --hidden --glob '!scripts/verify_public_boundary.sh' \
  'github\.com/nextmoca/nextmoca|src/core/agentbuilder|selective_state|evidence_path' "$ROOT"; then
  fail 'public adapter repository contains a private-monorepo or core-engine reference'
fi

printf '%s\n' 'PASS: public repository boundary is adapter-only'
