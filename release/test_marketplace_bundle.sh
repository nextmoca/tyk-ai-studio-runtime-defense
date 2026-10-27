#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MARKETPLACE="$ROOT/marketplace"
MANIFEST="$MARKETPLACE/manifest.yaml.tmpl"

require() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if ! grep -Fq -- "$pattern" "$file"; then
    printf 'FAIL: %s\n' "$message" >&2
    exit 1
  fi
}

for file in "$MANIFEST" "$MARKETPLACE/README.md" "$MARKETPLACE/CHANGELOG.md" "$MARKETPLACE/icon.png"; do
  if [ ! -f "$file" ]; then
    printf 'FAIL: Marketplace bundle is missing %s\n' "$file" >&2
    exit 1
  fi
done

require "$MANIFEST" 'id: "com.nextmoca.runtime-context-defense"' 'Marketplace ID must match the compiled plugin'
require "$MANIFEST" 'version: "{{VERSION}}"' 'Marketplace version must be release-time data'
require "$MANIFEST" 'repository: "nextmoca/tyk-ai-studio-runtime-defense-plugin"' 'Marketplace image repository must match the release workflow'
require "$MANIFEST" 'tag: "{{VERSION}}"' 'Marketplace tag must be release-time data'
require "$MANIFEST" 'digest: "{{DIGEST}}"' 'Marketplace must pin the immutable OCI digest'
require "$MANIFEST" '- "linux/amd64"' 'Marketplace must list linux/amd64'
require "$MANIFEST" '- "linux/arm64"' 'Marketplace must list linux/arm64'
require "$MANIFEST" 'category: "security"' 'Marketplace category must be security'
require "$MANIFEST" 'maturity: "beta"' 'Initial Marketplace release must be beta'
require "$MANIFEST" 'license: "Apache-2.0"' 'Marketplace license must be Apache-2.0'
require "$MANIFEST" '- "post_auth"' 'Marketplace hook must match the plugin'
require "$MANIFEST" 'services: []' 'Thin plugin must not request Tyk service permissions'
require "$MANIFEST" 'enterprise_only: false' 'Marketplace bundle must be Community Edition compatible'
require "$MARKETPLACE/README.md" 'oci://ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin@sha256:' 'Install documentation must use a digest-pinned OCI reference'
require "$ROOT/README.md" 'Marketplace release bundle' 'Plugin README must link to the Marketplace release instructions'

printf 'PASS: Marketplace submission bundle is release-safe\n'
