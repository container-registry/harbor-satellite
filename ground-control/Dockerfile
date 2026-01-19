# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy root module first (for the replace directive)
COPY go.mod go.sum ./
COPY pkg/ ./pkg/
COPY internal/ ./internal/
COPY cmd/ ./cmd/

# Copy ground-control module
COPY ground-control/ ./ground-control/

# Download dependencies and build
WORKDIR /app/ground-control
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /ground-control ./main.go

# Final stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /ground-control /app/ground-control

# Copy migrations
COPY --from=builder /app/ground-control/sql/schema /migrations

EXPOSE 8080

ENTRYPOINT ["/app/ground-control"]
