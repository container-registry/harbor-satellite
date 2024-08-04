#!/bin/sh

# Define Docker file path
DOCKERFILE_PATH="ci/satellite/Dockerfile"

# Build the Docker image
docker build -t satellite-app -f $DOCKERFILE_PATH .

# Run the Docker container
docker run --rm -p 8080:8080 satellite-app
