# Harbor Satellite Ground Control API Reference

Ground Control provides a REST API for managing satellites, groups, and configuration. This document provides comprehensive reference documentation for all available endpoints, including examples and request/response schemas.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Base URL](#base-url)
- [Error Handling](#error-handling)
- [Public Endpoints](#public-endpoints)
- [Protected Endpoints](#protected-endpoints)
  - [User Management](#user-management)
  - [Group Management](#group-management)
  - [Configuration Management](#configuration-management)
  - [Satellite Management](#satellite-management)

## Overview

Ground Control provides a RESTful API for managing Harbor Satellite deployments. All endpoints return JSON responses and use standard HTTP status codes. The API is organized around REST principles with predictable resource-oriented URLs.

## Authentication

Most endpoints require authentication using Bearer tokens. Include the token in the Authorization header:

```bash
Authorization: Bearer <session-token>
```

### Getting a Session Token

Obtain a session token via the login endpoint:

```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "your-password"
  }'
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2024-01-01T12:00:00Z"
}
```

## Base URL

The default Ground Control API is available at:
```
http://localhost:8080/api
```

Adjust the base URL according to your deployment configuration.

## Error Handling

All error responses follow a consistent format:

```json
{
  "error": "Error description",
  "code": "ERROR_CODE",
  "details": {}
}
```

**Common HTTP Status Codes:**
- `200 OK` - Success
- `201 Created` - Resource created successfully  
- `204 No Content` - Success with no response body
- `400 Bad Request` - Invalid request parameters
- `401 Unauthorized` - Authentication required or invalid
- `403 Forbidden` - Insufficient permissions
- `404 Not Found` - Resource not found
- `409 Conflict` - Resource conflict (e.g., duplicate name)
- `500 Internal Server Error` - Server error

## Public Endpoints

### Health Check

Check if Ground Control is running and healthy.

**Endpoint:** `GET /health`

**Example:**
```bash
curl http://localhost:8080/health
```

**Response:**
```
HTTP/1.1 200 OK
```

### Login

Authenticate and obtain a session token.

**Endpoint:** `POST /login`

**Request Body:**
```json
{
  "username": "admin",
  "password": "SecurePass123"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "SecurePass123"
  }'
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2024-01-01T12:00:00Z"
}
```

**Error Responses:**
- `401 Unauthorized` - Invalid credentials
- `429 Too Many Requests` - Rate limit exceeded

### Satellite Registration (ZTR)

Zero-touch registration endpoint for satellites to obtain configuration.

**Endpoint:** `GET /satellites/ztr/{token}`

**Path Parameters:**
- `token` (string, required): Satellite registration token

**Example:**
```bash
curl http://localhost:8080/satellites/ztr/satellite-token-here
```

**Response:**
```json
{
  "state": "harbor.example.com/satellite/satellite-state/my-satellite/state:latest",
  "auth": {
    "url": "https://harbor.example.com",
    "username": "robot_satellite_my-satellite",
    "password": "robot-account-secret"
  }
}
```

### Satellite Sync

Endpoint for satellites to report status and receive updates.

**Endpoint:** `POST /satellites/sync`

**Request Body:**
```json
{
  "name": "satellite_1",
  "activity": "idle",
  "state_report_interval": "@every 00h01m00s",
  "latest_state_digest": "sha256:abc123...",
  "latest_config_digest": "sha256:def456...",
  "memory_used_bytes": 1073741824,
  "storage_used_bytes": 2147483648,
  "cpu_percent": 25.5,
  "request_created_time": "2024-01-01T12:00:00Z",
  "last_sync_duration_ms": 1500,
  "image_count": 42
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/satellites/sync \
  -H "Content-Type: application/json" \
  -d '{
    "name": "satellite_1",
    "activity": "idle",
    "state_report_interval": "@every 00h01m00s",
    "memory_used_bytes": 1073741824,
    "storage_used_bytes": 2147483648,
    "cpu_percent": 25.5,
    "request_created_time": "2024-01-01T12:00:00Z",
    "image_count": 42
  }'
```

## Protected Endpoints

All protected endpoints require authentication via Bearer token.

### User Management

#### List Users

Get all users in the system.

**Endpoint:** `GET /api/users`

**Headers:**
```
Authorization: Bearer <token>
```

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/users
```

**Response:**
```json
[
  {
    "id": 1,
    "username": "admin",
    "role": "system_admin",
    "created_at": "2024-01-01T12:00:00Z"
  },
  {
    "id": 2,
    "username": "user1",
    "role": "admin",
    "created_at": "2024-01-01T13:00:00Z"
  }
]
```

#### Get User

Get details for a specific user.

**Endpoint:** `GET /api/users/{username}`

**Path Parameters:**
- `username` (string, required): Username to retrieve

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/users/admin
```

**Response:**
```json
{
  "id": 1,
  "username": "admin",
  "role": "system_admin",
  "created_at": "2024-01-01T12:00:00Z"
}
```

#### Create User

Create a new user (requires system_admin role).

**Endpoint:** `POST /api/users`

**Request Body:**
```json
{
  "username": "newuser",
  "password": "SecurePass123"
}
```

**Example:**
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username":"newuser","password":"SecurePass123"}' \
  http://localhost:8080/api/users
```

**Response:**
```json
{
  "id": 3,
  "username": "newuser",
  "role": "admin",
  "created_at": "2024-01-01T14:00:00Z"
}
```

### Group Management

#### List Groups

Get all groups with their artifact definitions.

**Endpoint:** `GET /api/groups`

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/groups
```

**Response:**
```json
[
  {
    "id": 1,
    "group_name": "production-apps",
    "registry_url": "https://harbor.example.com",
    "projects": ["library", "myapp"],
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

#### Get Group

Get details for a specific group.

**Endpoint:** `GET /api/groups/{group}`

**Path Parameters:**
- `group` (string, required): Group name

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/groups/production-apps
```

#### Create/Update Group

Create or update a group with artifact specifications.

**Endpoint:** `POST /api/groups/sync`

**Request Body:**
```json
{
  "group": "production-apps",
  "registry": "https://harbor.example.com",
  "artifacts": [
    {
      "repository": "library/nginx",
      "tag": ["latest", "1.21"],
      "type": "docker",
      "digest": "sha256:5a6ee6c36824...",
      "deleted": false
    },
    {
      "repository": "myapp/api",
      "tag": ["v1.0.0"],
      "type": "docker", 
      "digest": "sha256:7b8ff7d47829...",
      "deleted": false
    }
  ]
}
```

**Example:**
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "group": "production-apps",
    "registry": "https://harbor.example.com",
    "artifacts": [
      {
        "repository": "library/nginx",
        "tag": ["latest"],
        "type": "docker",
        "digest": "sha256:5a6ee6c36824...",
        "deleted": false
      }
    ]
  }' \
  http://localhost:8080/api/groups/sync
```

#### Get Group Satellites

List all satellites in a group.

**Endpoint:** `GET /api/groups/{group}/satellites`

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/groups/production-apps/satellites
```

#### Add Satellite to Group

Assign a satellite to a group.

**Endpoint:** `POST /api/groups/satellite`

**Request Body:**
```json
{
  "satellite": "satellite_1",
  "group": "production-apps"
}
```

**Example:**
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"satellite":"satellite_1","group":"production-apps"}' \
  http://localhost:8080/api/groups/satellite
```

#### Remove Satellite from Group

Remove a satellite from a group.

**Endpoint:** `DELETE /api/groups/satellite`

**Request Body:**
```json
{
  "satellite": "satellite_1", 
  "group": "production-apps"
}
```

### Configuration Management

#### List Configurations

Get all available configurations.

**Endpoint:** `GET /api/configs`

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/configs
```

**Response:**
```json
[
  {
    "id": 1,
    "config_name": "default-config",
    "registry_url": "https://harbor.example.com",
    "config": {
      "app_config": {
        "ground_control_url": "https://ground-control.example.com",
        "log_level": "info"
      }
    },
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

#### Get Configuration

Get a specific configuration by name.

**Endpoint:** `GET /api/configs/{config}`

**Path Parameters:**
- `config` (string, required): Configuration name

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/configs/default-config
```

#### Create Configuration

Create a new configuration.

**Endpoint:** `POST /api/configs`

**Request Body:**
```json
{
  "config_name": "edge-config",
  "config": {
    "state_config": {},
    "app_config": {
      "ground_control_url": "https://ground-control.example.com",
      "log_level": "info",
      "use_unsecure": false,
      "state_replication_interval": "@every 00h05m00s",
      "register_satellite_interval": "@every 00h01m00s",
      "heartbeat_interval": "@every 00h01m00s",
      "local_registry": {
        "url": "http://0.0.0.0:8585"
      }
    },
    "zot_config": {
      "distSpecVersion": "1.1.0",
      "storage": {
        "rootDirectory": "/var/lib/satellite/zot"
      },
      "http": {
        "address": "0.0.0.0",
        "port": "8585"
      },
      "log": {
        "level": "info"
      }
    }
  }
}
```

**Example:**
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @config-payload.json \
  http://localhost:8080/api/configs
```

#### Update Configuration

Update an existing configuration using JSON merge patch.

**Endpoint:** `PATCH /api/configs/{config}`

**Request Body (partial update):**
```json
{
  "app_config": {
    "log_level": "debug"
  }
}
```

#### Set Satellite Configuration

Assign a configuration to a satellite.

**Endpoint:** `POST /api/configs/satellite`

**Request Body:**
```json
{
  "satellite": "satellite_1",
  "config_name": "edge-config"
}
```

### Satellite Management

#### List Satellites

Get all registered satellites.

**Endpoint:** `GET /api/satellites`

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/satellites
```

**Response:**
```json
[
  {
    "id": 1,
    "name": "satellite_1",
    "created_at": "2024-01-01T12:00:00Z"
  },
  {
    "id": 2,
    "name": "edge-west-1",
    "created_at": "2024-01-01T13:00:00Z"
  }
]
```

#### Register Satellite

Register a new satellite and get its authentication token.

**Endpoint:** `POST /api/satellites`

**Request Body:**
```json
{
  "name": "edge-west-2",
  "groups": ["production-apps", "monitoring"],
  "config_name": "edge-config"
}
```

**Example:**
```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "edge-west-2",
    "groups": ["production-apps"],
    "config_name": "edge-config"
  }' \
  http://localhost:8080/api/satellites
```

**Response:**
```json
{
  "token": "satellite-auth-token-here"
}
```

> **Important**: Save the satellite token - it's needed for satellite startup and cannot be retrieved again.

#### Get Satellite Details

Get details for a specific satellite.

**Endpoint:** `GET /api/satellites/{satellite}`

**Path Parameters:**
- `satellite` (string, required): Satellite name

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/satellites/satellite_1
```

#### Get Satellite Status

Get the latest status report from a satellite.

**Endpoint:** `GET /api/satellites/{satellite}/status`

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/satellites/satellite_1/status
```

**Response:**
```json
{
  "id": 1,
  "satellite_id": 1,
  "activity": "syncing",
  "latest_state_digest": "sha256:abc123...",
  "latest_config_digest": "sha256:def456...",
  "cpu_percent": "15.5",
  "memory_used_bytes": 1073741824,
  "storage_used_bytes": 2147483648,
  "last_sync_duration_ms": 1200,
  "image_count": 25,
  "reported_at": "2024-01-01T12:05:00Z"
}
```

#### Get Cached Images

Get list of images cached on a satellite.

**Endpoint:** `GET /api/satellites/{satellite}/images`

**Example:**
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/satellites/satellite_1/images
```

**Response:**
```json
[
  {
    "repository": "library/nginx",
    "tag": "latest",
    "digest": "sha256:5a6ee6c36824...",
    "size_bytes": 142857216,
    "cached_at": "2024-01-01T12:00:00Z"
  }
]
```

#### Delete Satellite

Unregister a satellite and clean up its resources.

**Endpoint:** `DELETE /api/satellites/{satellite}`

**Example:**
```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/satellites/satellite_1
```

> **Warning**: This permanently deletes the satellite's robot account and state artifacts.

## Rate Limiting

API requests are rate limited to prevent abuse. Limits vary by endpoint type:

- **Login endpoints**: Strict rate limiting to prevent brute force attacks
- **General API endpoints**: Standard rate limiting for normal operations
- **Satellite sync endpoints**: Optimized for regular satellite heartbeats

When rate limits are exceeded, you'll receive a `429 Too Many Requests` response.

## Best Practices

### Authentication
- Store session tokens securely
- Implement token refresh before expiration
- Use HTTPS in production environments

### Error Handling
- Always check HTTP status codes
- Parse error response bodies for detailed messages
- Implement retries with exponential backoff for 5xx errors

### Polling
- Use appropriate intervals for satellite status checks
- Implement jitter to avoid thundering herd effects
- Cache responses when appropriate

## Next Steps

- **Getting Started** — [Initial setup guide](getting-started.md)
- **Configuration** — [Configuration reference](configuration.md) for all options
- **Architecture** — [System architecture](architecture.md) for design details
- **Troubleshooting** — [Common issues and solutions](troubleshooting.md)