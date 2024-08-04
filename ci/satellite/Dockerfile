# Use the official Go image as a base image
FROM golang:1.22 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the Go Modules manifests
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source code into the container
COPY . .

# Build the Go app
RUN go build -o satellite main.go

# Start a new stage from scratch
FROM scratch

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/satellite /satellite

# Command to run the executable
ENTRYPOINT ["/satellite"]
