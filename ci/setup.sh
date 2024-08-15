#!/bin/bash

# Function to handle errors
error_exit() {
    echo "Error: $1" >&2
    exit 1
}

# Function to retry a command with a delay
retry() {
    local retries=$1
    local delay=$2
    shift 2
    local cmd="$@"
    for i in $(seq 1 $retries); do
        echo "Attempt $i: Running command: $cmd"
        if $cmd; then
            return 0
        fi
        echo "Command failed, retrying in $delay seconds..."
        sleep $delay
    done
    return 1
}

# Check for root privileges
if [ "$(id -u)" -ne 0 ]; then
    error_exit "This script must be run as root"
fi

# Update the package index
retry 3 5 "apt-get update" || error_exit "Failed to update package index after multiple attempts."

# Install CA certificates and curl
retry 3 5 "apt-get install -y ca-certificates curl" || error_exit "Failed to install CA certificates and curl after multiple attempts."

# Install Docker using the rootless script
retry 3 5 "curl -fsSL https://get.docker.com/rootless -o get-docker-rootless.sh" || error_exit "Failed to download Docker rootless installation script after multiple attempts."
sh get-docker-rootless.sh || error_exit "Failed to install Docker in rootless mode."

# Set up Docker rootless environment
export PATH=$HOME/bin:$PATH
dockerd-rootless-setuptool.sh install || error_exit "Failed to set up Docker rootless environment."

# Verify Docker installation
docker --version || error_exit "Docker installation verification failed."

echo "Docker setup completed successfully."
