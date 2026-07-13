# Build stage
FROM golang:1.26.5-alpine AS builder

WORKDIR /app

# Install git for go mod download
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

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

# Copy binary and Ground Control migrations from builder. The migrations copy is
# harmless for the satellite image and keeps one Dockerfile for both binaries.
COPY --from=builder /app-bin /app/app
COPY --from=builder /app/internal/groundcontrol/sql/schema /migrations

# Create data directory
RUN mkdir -p /data

# Create non-root user
RUN adduser -D -g '' appuser
RUN chown -R appuser:appuser /data
USER appuser

WORKDIR /data

EXPOSE 8080 8585

ENTRYPOINT ["/app/app"]
