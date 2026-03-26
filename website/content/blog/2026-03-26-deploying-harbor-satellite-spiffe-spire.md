---
title: "Deploying Harbor Satellite with SPIFFE/SPIRE: A Practical Guide"
date: 2026-03-26T10:00:00+01:00
author: aloui-ikram
description: "A step-by-step tutorial on deploying Harbor Satellite with SPIFFE/SPIRE identity and validating air-gap image pulls."
tags:
  - harbor-satellite
  - spiffe
  - spire
  - edge-computing
  - air-gap
---

This is a practical walkthrough to deploy Harbor Satellite with SPIFFE/SPIRE and validate offline image pulls.

## Prerequisites

- Linux machine with Docker and Docker Compose
- `curl`, `grep`, `tar`, and `wget`
- Harbor Satellite repository cloned locally
- Harbor admin password available
- Open ports: `80`, `9080`, `5050`

## What you will achieve

- Run Central Harbor as source registry
- Start Ground Control and Edge Satellite
- Sync `nginx:alpine` to Satellite cache
- Prove local image pulls still work when central services are down

## Step 1: Set up Central Harbor Registry

```bash
# Download Harbor
wget https://github.com/goharbor/harbor/releases/download/v2.8.0/harbor-offline-installer-v2.8.0.tgz
tar xzvf harbor-offline-installer-v2.8.0.tgz
cd harbor

# Configure for your machine
nano harbor.yml
# Change: hostname = YOUR_IP_ADDRESS
# For local testing only, disable HTTPS block if needed

# Install
sudo ./install.sh
# Expected: Harbor services start successfully
```

## Step 2: Start Ground Control

```bash
cd deploy/quickstart/spiffe/join-token/external/gc
HARBOR_URL=http://<YOUR_HARBOR_IP>:80 ./setup.sh
# Expected: Ground Control is running and connected to Harbor
```

## Step 3: Start the Edge Satellite

```bash
cd ../sat
./setup.sh
# Expected: Satellite starts and authenticates using SPIFFE/SPIRE
```

## Step 4: Verify everything is running

```bash
# Ground Control health
curl -k https://localhost:9080/health
# Expected: ok

# Edge Satellite registry endpoint
curl -i http://localhost:5050/v2/
# Expected: HTTP/1.1 200 OK
```

## Step 5: Tell Satellite to cache images

```bash
# 1) Login and get auth token
TOKEN=$(curl -sk -X POST "https://localhost:9080/login" \
  -d '{"username":"admin","password":"<HARBOR_PASSWORD>"}' | \
  grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# 2) Get nginx:alpine digest from Harbor
DIGEST=$(curl -sk -u "admin:<HARBOR_PASSWORD>" \
  "http://<YOUR_HARBOR_IP>/api/v2.0/projects/library/repositories/nginx/artifacts?q=tags%3Dalpine&page_size=1" | \
  grep -m1 '"digest":' | cut -d'"' -f4)
```

```bash
# 3) Create edge group and sync policy
curl -sk -X POST "https://localhost:9080/api/groups/sync" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d "{
    \"group\": \"edge-group\",
    \"registry\": \"http://<YOUR_HARBOR_IP>:80\",
    \"artifacts\": [{\"repository\": \"library/nginx\", \"tag\": [\"alpine\"], \"type\": \"image\", \"digest\": \"${DIGEST}\"}]
  }"

# 4) Assign Satellite to group
curl -sk -X POST "https://localhost:9080/api/groups/satellite" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{"satellite":"edge-01","group":"edge-group"}'
# Expected: after ~30-45s, image is cached on Satellite
```

```bash
# Verify cache catalog
curl -s http://localhost:5050/v2/_catalog
# Expected: {"repositories":["library/nginx"]}
```

## Testing without internet (air-gap validation)

### Step 1: Clear local cache

```bash
docker rmi localhost:5050/library/nginx:alpine
# If image does not exist locally, Docker prints an error and continues
```

### Step 2: Stop central services

```bash
docker stop harbor ground-control
# Expected: central side is unavailable
```

### Step 3: Pull from local offline cache

```bash
docker pull localhost:5050/library/nginx:alpine
# Expected: pull still succeeds from local Satellite cache
```
