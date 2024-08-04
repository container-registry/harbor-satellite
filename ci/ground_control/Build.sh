#!/bin/sh

# Build the Docker image
docker build -t ground_control_ci -f Dockerfile .

# Run the Docker container
docker run -p 8080:8080 ground_control_ci
