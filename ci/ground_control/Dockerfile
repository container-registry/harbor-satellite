# Use the official Golang image to create a build artifact.
FROM golang:1.22 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy the source from the current directory to the Working Directory inside the container
COPY ground-control/ .

# Build the Go app
RUN go build -o ground_control main.go

# Use a minimal image to reduce the attack surface and dependencies
FROM scratch

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/ground_control /ground_control

# Command to run the executable
ENTRYPOINT ["/ground_control"]
