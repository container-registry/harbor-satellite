#!/bin/sh

# Define Docker file path
DOCKERFILE_PATH="ci/satellite/Dockerfile"

# Build the Docker image
if ! docker build -t satellite-app -f $DOCKERFILE_PATH .; then
  echo "Docker build failed"
  exit 1
fi

# Run the Docker container
if ! docker run --rm -p 8080:8080 satellite-app; then
  echo "Docker run failed"
  exit 1
fi
