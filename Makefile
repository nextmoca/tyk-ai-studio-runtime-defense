.PHONY: test build build-linux-amd64 build-linux-arm64 docker-build oci-build oci-verify

OCI_IMAGE ?= ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin:local
OCI_PLATFORM ?= linux/amd64
OCI_VERSION ?= dev
OCI_REVISION ?= $(shell git rev-parse --short HEAD)

test:
	go test ./...

build:
	go build ./cmd/nextmoca-tyk-plugin

build-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/nextmoca-tyk-plugin-linux-amd64 ./cmd/nextmoca-tyk-plugin

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/nextmoca-tyk-plugin-linux-arm64 ./cmd/nextmoca-tyk-plugin

docker-build:
	docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/nextmoca/tyk-ai-studio-runtime-defense-plugin:dev .

oci-build:
	docker buildx build --platform $(OCI_PLATFORM) --load \
		--build-arg VERSION=$(OCI_VERSION) \
		--build-arg REVISION=$(OCI_REVISION) \
		--tag $(OCI_IMAGE) .

oci-verify: oci-build
	@docker image inspect $(OCI_IMAGE) --format '{{json .Config.Entrypoint}}' | grep -F '["/nextmoca-tyk-plugin"]' >/dev/null
	@container_id=$$(docker create $(OCI_IMAGE)); \
	trap 'docker rm -f $$container_id >/dev/null' EXIT; \
	docker export $$container_id | tar -tf - | grep -Fx 'nextmoca-tyk-plugin' >/dev/null; \
	if docker export $$container_id | tar -tf - | grep -Eq '^(src/|go.mod|go.sum|tests/)'; then \
		echo 'FAIL: runtime image contains build source'; exit 1; \
	fi
	@echo 'PASS: OCI image contains only the runtime plugin artifact'
