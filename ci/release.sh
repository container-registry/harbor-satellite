#!/bin/bash

function check_if_success() {
    if [ $? -ne 0 ]; then
        echo "Error running last command"
        exit $?
    fi
}

function print_help() {
    echo "Usage:
    ./release.sh

Make sure the following environment variables are set:
    GITHUB_API_TOKEN   - Your GitHub API token
    VERSION            - The tag name for the release (e.g., v1.0.0)
    REPO_OWNER         - The GitHub username or organization name
    REPO_NAME          - The repository name
    RELEASE_NAME       - The name of the release (e.g., \"Release v1.0.0\")
    OUT_DIR            - Directory containing the pre-built binaries
    PRERELEASE         - Set to 'true' for a prerelease, otherwise leave empty or set to 'false'

Example:
    export GITHUB_API_TOKEN=your_token
    export VERSION=v1.0.0
    export REPO_OWNER=your_username
    export REPO_NAME=your_repo
    export RELEASE_NAME=\"Release v1.0.0\"
    export OUT_DIR=./bin
    export PRERELEASE=false
    ./release.sh"
}

if [ "$1" == "--help" ] || [ "-h" == "$1" ]; then
    print_help
    exit 0
fi

# Check for required environment variables
if [ -z "$GITHUB_API_TOKEN" ] || [ -z "$VERSION" ] || [ -z "$REPO_OWNER" ] || [ -z "$REPO_NAME" ] || [ -z "$RELEASE_NAME" ] || [ -z "$OUT_DIR" ]; then
    echo "Missing one or more required environment variables."
    print_help
    exit 1
fi

echo "Tag Name: $VERSION"
echo "Release Name: $RELEASE_NAME"
echo "Prerelease: $PRERELEASE"
echo "Repo Owner: $REPO_OWNER"
echo "Repo Name: $REPO_NAME"

# Ensure PRERELEASE is a valid boolean value
if [ "$PRERELEASE" = "true" ]; then
    PRERELEASE_JSON="true"
else
    PRERELEASE_JSON="false"
fi

# Create GitHub release
GITHUB_URL="https://api.github.com/repos/$REPO_OWNER/$REPO_NAME/releases"
JSON=$(cat <<EOF
{
  "tag_name": "$VERSION",
  "name": "$RELEASE_NAME",
  "body": "$RELEASE_NAME",
  "prerelease": $PRERELEASE_JSON
}
EOF
)

echo "Creating GitHub release..."
RESP=$(curl -s -X POST --data "$JSON" -H "Content-Type: application/json" -H "Authorization: token $GITHUB_API_TOKEN" $GITHUB_URL)
check_if_success

UPLOAD_URL=$(echo "$RESP" | sed -n 's/.*"upload_url": "\([^{]*\){.*/\1/p')
if [ -z "$UPLOAD_URL" ]; then
    echo "Failed to create release or extract upload URL."
    echo "Response: $RESP"
    exit 1
fi

# Upload the binaries
for pkg in "$OUT_DIR"/*
do
    echo "Uploading $pkg"
    FILENAME=$(basename "$pkg")
    UPLOAD=$(curl -s --data-binary @"$pkg" -H "Content-Type: application/octet-stream" -H "Authorization: token $GITHUB_API_TOKEN" "$UPLOAD_URL?name=$FILENAME")
    check_if_success
done

echo "Release $RELEASE_NAME deployed successfully."
