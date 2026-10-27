FROM golang:1.26.6@sha256:0d1d3a794be25f809dd2cb3160d8c73276c4056a9f8242a138e908ddeee7b6b6 AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/ai-studio-enterprise-placeholder ./third_party/ai-studio-enterprise-placeholder
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/nextmoca-tyk-plugin ./cmd/nextmoca-tyk-plugin

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE=https://github.com/nextmoca/tyk-ai-studio-runtime-defense
LABEL org.opencontainers.image.title="Next Moca Runtime Defense for AI Agents" \
    org.opencontainers.image.description="Thin Tyk AI Studio plugin adapter for hosted Next Moca runtime context defense." \
    org.opencontainers.image.source="${SOURCE}" \
    org.opencontainers.image.revision="${REVISION}" \
    org.opencontainers.image.version="${VERSION}" \
    org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/nextmoca-tyk-plugin /nextmoca-tyk-plugin
USER nonroot:nonroot
ENTRYPOINT ["/nextmoca-tyk-plugin"]
