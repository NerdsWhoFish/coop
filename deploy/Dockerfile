# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

WORKDIR /src

# Dependencies change far less often than source, so they get their own layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w \
        -X github.com/nerdswhofish/coop/internal/version.Version=${VERSION} \
        -X github.com/nerdswhofish/coop/internal/version.Commit=${COMMIT} \
        -X github.com/nerdswhofish/coop/internal/version.Date=${DATE}" \
      -o /out/coopd ./cmd/coopd

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/coopd /usr/local/bin/coopd

# Numeric so Kubernetes runAsNonRoot can verify it without a passwd lookup.
USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/coopd"]
CMD ["serve"]
