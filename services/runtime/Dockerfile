# syntax=docker/dockerfile:1.7
# Multi-stage build for the af-stack runtime binary.
#
# Stage 1: download deps and build the static binary.
# Stage 2: minimal distroless runtime.

FROM golang:1.26-alpine AS build
RUN apk add --no-cache git ca-certificates

WORKDIR /src
# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static build, stripped, no CGO
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=$(cat .version 2>/dev/null || echo dev)" \
    -o /out/af-stack \
    ./services/runtime/cmd/af-stack

# Stage 2: distroless runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/af-stack /usr/local/bin/af-stack
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /src/brand.yaml /app/brand.yaml

EXPOSE 8080 9090
USER nonroot:nonroot
# Phase 14.3: exec form is REQUIRED so SIGTERM reaches the af-stack
# process directly. Shell form (no brackets) would interpose /bin/sh,
# which swallows signals — Kubernetes would then escalate to SIGKILL
# after terminationGracePeriodSeconds and graceful drain would never
# happen.
ENTRYPOINT ["/usr/local/bin/af-stack"]
