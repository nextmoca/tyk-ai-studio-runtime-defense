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

if [ ! -f "$ROOT/cosign.pub" ]; then
  printf 'FAIL: Enterprise OCI plugin releases require cosign.pub\n' >&2
  exit 1
fi

require 'tags:' 'workflow must publish versioned plugin tags'
require '- "v*"' 'workflow must publish only semver-style plugin tags'
require '[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]' 'workflow must reject non-semver release tags'
require 'manifest_version=$(jq -er' 'workflow must read the compiled plugin manifest version'
require 'test "$manifest_version" = "$version"' 'workflow must require the tag and plugin manifest versions to match'
require 'packages: write' 'workflow must be allowed to publish to GHCR'
require 'oras-project/setup-oras@v1' 'workflow must install ORAS 1.3+'
require 'version: 1.3.0' 'workflow must pin an ORAS version that can create an index'
require 'oras login ghcr.io' 'workflow must authenticate ORAS to GHCR'
require 'CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build' 'workflow must cross-compile static plugin binaries'
require 'oras push "$IMAGE:$version-linux_$arch"' 'workflow must publish one artifact per Linux architecture'
require '--artifact-type application/vnd.tyk.plugin.binary.v1' 'workflow must identify the Tyk plugin artifact type'
require '--artifact-platform linux/$arch' 'workflow must tag each artifact with its host platform'
require 'application/vnd.tyk.plugin.layer.v1' 'plugin binary must be the artifact layer'
require 'oras manifest index create "$IMAGE:$version"' 'workflow must create a multi-platform artifact index'
require 'oras manifest fetch --descriptor' 'workflow must record the immutable index digest'
require 'sigstore/cosign-installer@v3' 'workflow must install Cosign'
require 'COSIGN_PRIVATE_KEY' 'workflow must use the repository signing key'
require 'COSIGN_PASSWORD' 'workflow must use the repository signing-key password'
require 'cosign sign --yes --key env://COSIGN_KEY' 'workflow must sign the immutable index digest with the key pair'
require '--new-bundle-format=false' 'workflow must publish a legacy-compatible signature'
require 'cosign verify --key cosign.pub' 'workflow must verify the published signature'
require 'ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin' 'workflow must publish the designated thin-plugin artifact'

if grep -Eq 'docker/(setup-buildx-action|build-push-action)|provenance:|sbom:' "$WORKFLOW"; then
  printf 'FAIL: OCI plugin workflow must not publish a Docker container image\n' >&2
  exit 1
fi

printf 'PASS: OCI release workflow contract is satisfied\n'
