#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ORAS="${ORAS:-oras}"
VERSION="0.1.3-test"
WORKDIR="$(mktemp -d)"
LAYOUT="$WORKDIR/layout"

cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

command -v "$ORAS" >/dev/null || {
  printf 'FAIL: ORAS 1.3+ is required; set ORAS to its executable path\n' >&2
  exit 1
}

for arch in amd64 arm64; do
  binary="$WORKDIR/nextmoca-tyk-plugin-linux_$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' \
    -o "$binary" "$ROOT/cmd/nextmoca-tyk-plugin"
  chmod +x "$binary"
  (
    cd "$WORKDIR"
    "$ORAS" push "$LAYOUT:$VERSION-linux_$arch" --oci-layout \
      --artifact-type application/vnd.tyk.plugin.binary.v1 \
      --artifact-platform "linux/$arch" \
      "./nextmoca-tyk-plugin-linux_$arch:application/vnd.tyk.plugin.layer.v1" >/dev/null
  )
done

"$ORAS" manifest index create "$LAYOUT:$VERSION" \
  "$VERSION-linux_amd64" "$VERSION-linux_arm64" --oci-layout >/dev/null

index="$("$ORAS" manifest fetch "$LAYOUT:$VERSION" --oci-layout)"
test "$(printf '%s' "$index" | jq '.manifests | length')" = "2"
test "$(printf '%s' "$index" | jq -c '[.manifests[].platform.architecture] | sort')" = '["amd64","arm64"]'

for arch in amd64 arm64; do
  binary="$WORKDIR/nextmoca-tyk-plugin-linux_$arch"
  manifest="$("$ORAS" manifest fetch "$LAYOUT:$VERSION-linux_$arch" --oci-layout)"
  test "$(printf '%s' "$manifest" | jq '.layers | length')" = "1"
  test "$(printf '%s' "$manifest" | jq -r '.artifactType')" = 'application/vnd.tyk.plugin.binary.v1'
  test "$(printf '%s' "$manifest" | jq -r '.layers[0].mediaType')" = 'application/vnd.tyk.plugin.layer.v1'
  digest="$(printf '%s' "$manifest" | jq -r '.layers[0].digest')"
  cmp "$binary" "$LAYOUT/blobs/sha256/${digest#sha256:}"
done

printf '%s\n' 'PASS: OCI layout contains one executable binary layer per platform and a two-platform index'
