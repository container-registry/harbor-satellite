# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git for go mod download (if using private repos)
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /ground-control ./main.go

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /ground-control /app/ground-control

# Copy migrations (migrator looks at /migrations first, then sql/schema)
COPY --from=builder /app/sql/schema /migrations

# Create non-root user
RUN adduser -D -g '' appuser
USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/ground-control"]
