#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKFLOW="$ROOT/.github/workflows/oci-release.yml"

require() {
  local pattern="$1"
  local message="$2"
  if ! grep -Fq -- "$pattern" "$WORKFLOW"; then
    printf 'FAIL: %s\n' "$message" >&2
    exit 1
  fi
}

if [ ! -f "$WORKFLOW" ]; then
  printf 'FAIL: Tyk OCI release workflow is missing\n' >&2
  exit 1
fi

require 'tags:' 'workflow must publish versioned plugin tags'
require '- "v*"' 'workflow must publish only semver-style plugin tags'
require '[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]' 'workflow must reject non-semver release tags'
require 'manifest_version=$(jq -er' 'workflow must read the compiled plugin manifest version'
require 'test "$manifest_version" = "$version"' 'workflow must require the tag and plugin manifest versions to match'
require 'packages: write' 'workflow must be allowed to publish to GHCR'
require 'id-token: write' 'workflow must be allowed to request an OIDC identity for signing'
require 'docker/setup-buildx-action@v3' 'workflow must use Docker Buildx'
require 'linux/amd64,linux/arm64' 'workflow must publish both supported Linux architectures'
require 'provenance: mode=max' 'workflow must publish provenance'
require 'sbom: true' 'workflow must publish an SBOM'
require 'docker/build-push-action@v6' 'workflow must build and push the OCI image'
require 'docker buildx imagetools inspect' 'workflow must verify the multi-architecture index'
require 'sigstore/cosign-installer@v3' 'workflow must install Cosign'
require 'cosign sign --yes' 'workflow must keylessly sign the immutable digest'
require 'ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin' 'workflow must publish the designated thin-plugin image'

printf 'PASS: OCI release workflow contract is satisfied\n'
