#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKERFILE="$ROOT/Dockerfile"
DOCKERIGNORE="$ROOT/.dockerignore"
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

require "$DOCKERFILE" 'AS build' 'Dockerfile must use a dedicated build stage'
if [ "$(grep -Fc '@sha256:' "$DOCKERFILE")" -lt 2 ]; then
  printf 'FAIL: both Docker base images must be pinned by digest\n' >&2
  exit 1
fi
require "$DOCKERFILE" 'ARG TARGETOS=linux' 'Dockerfile must accept TARGETOS'
require "$DOCKERFILE" 'ARG TARGETARCH' 'Dockerfile must accept TARGETARCH'
require "$DOCKERFILE" 'CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}' 'Dockerfile must cross-compile a static target binary'
require "$DOCKERFILE" 'org.opencontainers.image.source' 'Dockerfile must expose OCI source metadata'
require "$DOCKERFILE" 'org.opencontainers.image.revision' 'Dockerfile must expose OCI revision metadata'
require "$DOCKERFILE" 'ENTRYPOINT ["/nextmoca-tyk-plugin"]' 'Runtime entrypoint must be the plugin binary'
require "$DOCKERFILE" 'USER nonroot:nonroot' 'Runtime image must not run as root'

for pattern in '.git' '.github' 'bin' '.env' '*.pem' 'release' 'marketplace' 'tests'; do
  require "$DOCKERIGNORE" "$pattern" ".dockerignore must exclude $pattern"
done

require "$MAKEFILE" 'oci-build' 'Makefile must offer an OCI build target'
require "$MAKEFILE" 'oci-verify' 'Makefile must offer an OCI verification target'

printf 'PASS: OCI build layout is release-safe\n'
