#!/bin/bash

set -e

# Check if required environment variables are set
if [ -z "$HARBOR_URL" ] || [ -z "$HARBOR_USERNAME" ] || [ -z "$HARBOR_PASSWORD" ]; then
    echo "Error: HARBOR_URL, HARBOR_USERNAME, and HARBOR_PASSWORD environment variables must be set"
    echo "Example:"
    echo "  export HARBOR_URL=https://your-harbor-instance"
    echo "  export HARBOR_USERNAME=admin"
    echo "  export HARBOR_PASSWORD=your-admin-password"
    exit 1
fi

ROBOT_NAME="ground-control-robot"
HARBOR_API_URL="${HARBOR_URL}/api/v2.0"

echo "Creating robot account for Ground Control operations..."

# Login and get cookie for authentication
COOKIE_JAR="harbor-cookie.txt"
curl -s -c "$COOKIE_JAR" -X POST "${HARBOR_API_URL}/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${HARBOR_USERNAME}\",\"password\":\"${HARBOR_PASSWORD}\"}"

# Create robot account with system level permissions
response=$(curl -s -b "$COOKIE_JAR" -X POST "${HARBOR_API_URL}/robots" \
    -H "Content-Type: application/json" \
    -d "{
        \"name\": \"${ROBOT_NAME}\",
        \"description\": \"Robot account for Ground Control operations\",
        \"level\": \"system\",
        \"disable\": false,
        \"duration\": -1,
        \"permissions\": [
            {
                \"kind\": \"system\",
                \"namespace\": \"/\",
                \"access\": [
                    {\"resource\": \"project\", \"action\": \"create\"},
                    {\"resource\": \"project\", \"action\": \"read\"},
                    {\"resource\": \"project\", \"action\": \"update\"},
                    {\"resource\": \"repository\", \"action\": \"pull\"},
                    {\"resource\": \"repository\", \"action\": \"push\"},
                    {\"resource\": \"artifact\", \"action\": \"read\"},
                    {\"resource\": \"artifact\", \"action\": \"list\"}
                ]
            }
        ]
    }")

# Clean up cookie file
rm -f "$COOKIE_JAR"

# Extract robot credentials from response
ROBOT_ID=$(echo $response | grep -o '"id":[0-9]*' | grep -o '[0-9]*')
ROBOT_SECRET=$(echo $response | grep -o '"secret":"[^"]*"' | grep -o ':"[^"]*"' | cut -d'"' -f2)
ROBOT_FULL_NAME=$(echo $response | grep -o '"name":"[^"]*"' | grep -o ':"[^"]*"' | cut -d'"' -f2)

if [ -z "$ROBOT_SECRET" ]; then
    echo "Error creating robot account. Server response:"
    echo $response
    exit 1
fi

echo "✅ Robot account created successfully!"
echo "Robot ID: $ROBOT_ID"
echo "Robot Name: $ROBOT_FULL_NAME"
echo "Robot Secret: $ROBOT_SECRET"
echo
echo "To use these credentials with Ground Control, set these environment variables:"
echo "export HARBOR_USERNAME=$ROBOT_FULL_NAME"
echo "export HARBOR_PASSWORD=$ROBOT_SECRET"
echo
echo "Verifying robot account permissions..."

# Verify robot account by making a test API call
VERIFY_RESPONSE=$(curl -s -u "${ROBOT_FULL_NAME}:${ROBOT_SECRET}" "${HARBOR_API_URL}/projects")
if [ $? -eq 0 ]; then
    echo "✅ Verification successful! Robot account has proper permissions."
else
    echo "❌ Verification failed. Robot account may not have sufficient permissions."
    echo $VERIFY_RESPONSE
fi