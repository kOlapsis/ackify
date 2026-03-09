# syntax=docker/dockerfile:1.7

# Build the SPA with the build platform to avoid slow QEMU emulation on arm64
FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS spa-builder

WORKDIR /app/webapp
COPY webapp/package*.json ./
# Speed up and stabilize npm installs in CI
# - no-audit/no-fund: skip network calls
# - no-progress: cleaner logs
# - cache mount: reuse npm cache between builds
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund --no-progress
COPY webapp/ ./
# Enable code instrumentation for E2E coverage if requested
ARG CYPRESS_COVERAGE=false
ENV CYPRESS_COVERAGE=$CYPRESS_COVERAGE
RUN npm run build

FROM golang:alpine AS builder

RUN apk update && apk add --no-cache ca-certificates git curl && rm -rf /var/cache/apk/*
RUN adduser -D -g '' ackuser

WORKDIR /app
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=auto
# Cache Go modules and build cache between builds
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download && go mod verify
COPY backend/ ./backend/

RUN mkdir -p backend/cmd/community/web/dist
COPY --from=spa-builder /app/webapp/dist ./backend/cmd/community/web/dist

# Cross-compile per target platform
ARG TARGETOS
ARG TARGETARCH
ARG VERSION="dev"
ARG COMMIT="unknown"
ARG BUILD_DATE="unknown"

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -a -installsuffix cgo \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
    -o /app/ackify ./backend/cmd/community

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o /app/migrate ./backend/cmd/migrate

# Create storage directory with correct ownership for nonroot user (UID 65532)
RUN mkdir -p /data/documents && chown -R 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION="dev"

LABEL maintainer="Benjamin TOUCHARD"
LABEL version="${VERSION}"
LABEL description="Ackify - Document signature validation platform"
LABEL org.opencontainers.image.source="https://github.com/kolapsis/ackify"
LABEL org.opencontainers.image.description="Professional solution for validating and tracking document reading"
LABEL org.opencontainers.image.licenses="AGPL-3.0-or-later"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app
COPY --from=builder /app/ackify /app/ackify
COPY --from=builder /app/migrate /app/migrate
COPY --from=builder /app/backend/migrations /app/migrations
COPY --from=builder /app/backend/locales /app/locales
COPY --from=builder /app/backend/templates /app/templates
COPY --from=builder /app/backend/openapi.yaml /app/openapi.yaml

# Copy storage directory with correct ownership (for volume initialization)
COPY --from=builder --chown=65532:65532 /data /data

ENV ACKIFY_TEMPLATES_DIR=/app/templates
ENV ACKIFY_LOCALES_DIR=/app/locales

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/ackify", "health"]

ENTRYPOINT ["/app/ackify"]
