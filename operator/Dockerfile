FROM golang:1.24-alpine AS builder

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o operator main.go

FROM alpine:3.20

RUN adduser -D appuser
USER appuser

COPY --from=builder /workspace/operator /operator

ENTRYPOINT ["/operator"]
