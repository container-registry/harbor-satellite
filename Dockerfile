# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /satellite ./cmd/main.go

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

# Copy binary from builder
COPY --from=builder /satellite /app/satellite

# Create data directory
RUN mkdir -p /data

# Create non-root user
RUN adduser -D -g '' appuser
RUN chown -R appuser:appuser /data
USER appuser

WORKDIR /data

EXPOSE 8585

ENTRYPOINT ["/app/satellite"]
