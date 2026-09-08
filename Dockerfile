# Global build args must be declared before the first FROM to be usable in
# FROM directives (Docker multi-stage scoping rule).
#
#   Prebuilt path:    docker buildx build --build-arg BUILDER_MODE=prebuilt
#                                         --build-arg BINARY_NAME=satellite ...
#                     (expects bin/linux-${TARGETARCH}/${BINARY_NAME})
#   From-source path: docker buildx build ...   (default; BUILDER_MODE=source)
ARG BUILDER_MODE=source

# ── Prebuilt path ────────────────────────────────────────────────────────────
# Used when a cross-compiled binary is supplied via the build context.
# Only installs ca-certificates; no Go toolchain, no module download, no
# source copy.
FROM alpine:3.20 AS builder-prebuilt

RUN apk add --no-cache ca-certificates

ARG TARGETARCH
ARG BINARY_NAME=satellite
COPY --chmod=755 bin/linux-${TARGETARCH}/${BINARY_NAME} /app-bin

# ── From-source path ─────────────────────────────────────────────────────────
# Full in-Docker Go build. Git is required by go mod download for
# VCS-stamped dependencies.
FROM golang:1.26.5-alpine AS builder-source

WORKDIR /app

RUN apk add --no-cache git ca-certificates

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary. Default: no extra tags (production-equivalent of main).
# Pass --build-arg GO_TAGS=parsec to opt into the PARSEC code path.
ARG GO_TAGS=""
ARG COMPONENT=satellite
RUN CGO_ENABLED=0 GOOS=linux go build -tags "${GO_TAGS}" -o /app-bin ./cmd/${COMPONENT}

# ── Builder selector ──────────────────────────────────────────────────────────
# BUILDER_MODE is declared globally above (before the first FROM) so Docker
# can evaluate it here. Defaults to 'source'; pass --build-arg BUILDER_MODE=prebuilt
# to select the lightweight prebuilt stage.
#
# DL3006 does not apply: this FROM names a prior stage (builder-source or
# builder-prebuilt), not an untagged registry image.
# hadolint ignore=DL3006
FROM builder-${BUILDER_MODE} AS builder

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

# Copy the binary from whichever builder stage was selected, and migrations
# from the build context (so the Go build is never forced in prebuilt mode).
COPY --from=builder /app-bin /app/app
# Copy migrations directly from the build context (not from builder-source) so
# the Go build stage is never forced to run in prebuilt mode.
COPY internal/groundcontrol/sql/schema /migrations

# Create data directory
RUN mkdir -p /data

# Create non-root user
RUN adduser -D -g '' appuser
RUN chown -R appuser:appuser /data
USER appuser

WORKDIR /data

EXPOSE 8080

ENTRYPOINT ["/app/app"]
