.PHONY: test build build-linux-amd64 build-linux-arm64 verify-linux-binaries verify-oci-artifact

test:
	go test ./...

build:
	go build ./cmd/nextmoca-tyk-plugin

build-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/nextmoca-tyk-plugin-linux-amd64 ./cmd/nextmoca-tyk-plugin

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/nextmoca-tyk-plugin-linux-arm64 ./cmd/nextmoca-tyk-plugin

verify-linux-binaries: build-linux-amd64 build-linux-arm64
	@test -s bin/nextmoca-tyk-plugin-linux-amd64
	@test -s bin/nextmoca-tyk-plugin-linux-arm64
	@echo 'PASS: static Linux plugin binaries are ready for ORAS artifact publication'

verify-oci-artifact:
	bash release/test_oci_artifact_layout.sh
