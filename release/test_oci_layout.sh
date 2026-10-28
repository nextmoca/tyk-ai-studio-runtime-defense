#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAKEFILE="$ROOT/Makefile"

require() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if ! grep -Fq -- "$pattern" "$file"; then
    printf 'FAIL: %s\n' "$message" >&2
    exit 1
  fi
}

if [ -e "$ROOT/Dockerfile" ] || [ -e "$ROOT/.dockerignore" ]; then
  printf 'FAIL: Tyk plugins must be published as binary OCI artifacts, not Docker images\n' >&2
  exit 1
fi

require "$MAKEFILE" 'build-linux-amd64' 'Makefile must offer a linux/amd64 static binary target'
require "$MAKEFILE" 'build-linux-arm64' 'Makefile must offer a linux/arm64 static binary target'
require "$MAKEFILE" 'CGO_ENABLED=0 GOOS=linux GOARCH=amd64' 'amd64 target must be statically compiled'
require "$MAKEFILE" 'CGO_ENABLED=0 GOOS=linux GOARCH=arm64' 'arm64 target must be statically compiled'

printf 'PASS: OCI build layout is release-safe\n'
