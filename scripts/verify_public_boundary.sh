#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

for path in \
  go.mod \
  Dockerfile \
  LICENSE \
  README.md \
  manifest.json \
  config.schema.json \
  .github/workflows/oci-release.yml \
  marketplace/manifest.yaml.tmpl \
  marketplace/README.md \
  marketplace/CHANGELOG.md \
  marketplace/icon.png; do
  [ -f "$ROOT/$path" ] || fail "missing public-plugin artifact: $path"
done

grep -Fqx 'module github.com/nextmoca/tyk-ai-studio-runtime-defense' "$ROOT/go.mod" \
  || fail 'Go module must identify the standalone public repository'

grep -Fq 'ARG SOURCE=https://github.com/nextmoca/tyk-ai-studio-runtime-defense' "$ROOT/Dockerfile" \
  || fail 'OCI source metadata must point to the standalone public repository'

grep -Fq '"version": "0.1.2"' "$ROOT/manifest.json" \
  || fail 'release must identify as 0.1.2'

grep -Fq 'ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin' "$ROOT/.github/workflows/oci-release.yml" \
  || fail 'release workflow must publish the designated adapter image'

grep -Fq 'https://github.com/nextmoca/tyk-ai-studio-runtime-defense' "$ROOT/marketplace/manifest.yaml.tmpl" \
  || fail 'Marketplace metadata must link to the standalone public repository'

if rg -n --hidden --glob '!scripts/verify_public_boundary.sh' \
  'github\.com/nextmoca/nextmoca|src/core/agentbuilder|selective_state|evidence_path' "$ROOT"; then
  fail 'public adapter repository contains a private-monorepo or core-engine reference'
fi

printf '%s\n' 'PASS: public repository boundary is adapter-only'
