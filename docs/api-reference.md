# Ground Control API Reference

This document provides a comprehensive reference for all Ground Control API endpoints.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Base URL](#base-url)
- [Error Handling](#error-handling)
- [Public Endpoints](#public-endpoints)
- [Protected Endpoints](#protected-endpoints)
  - [Authentication](#authentication-endpoints)
  - [Users](#user-management)
  - [Groups](#groups)
  - [Configs](#configs)
  - [Satellites](#satellites)

## Overview

Ground Control provides a RESTful API for managing Harbor Satellite deployments. All endpoints return JSON responses and use standard HTTP status codes.

## Authentication

Most endpoints require authentication using Bearer tokens. There are two types of tokens:

1. **Session Tokens**: Used for admin/user authentication. Obtained via the `/login` endpoint.
2. **Satellite Tokens (ZTR)**: Used for satellite authentication. Obtained when registering a satellite.

### Using Session Tokens

Include the token in the `Authorization` header:

```bash
Authorization: Bearer <session-token>
```

### Using Satellite Tokens

Satellite tokens are used in specific endpoints:
- `/satellites/ztr/{token}` - Get satellite configuration
- `/satellites/sync` - Satellite heartbeat (uses token in request body)

## Base URL

The default base URL is `http://localhost:9090`. Adjust this based on your deployment.

## Error Handling

All errors follow this format:

```json
{
  "error": "Error message"
}
```

Common HTTP status codes:
- `200 OK` - Success
- `201 Created` - Resource created
- `204 No Content` - Success with no response body
- `400 Bad Request` - Invalid request
- `401 Unauthorized` - Authentication required
- `403 Forbidden` - Insufficient permissions
- `404 Not Found` - Resource not found
- `409 Conflict` - Resource already exists
- `500 Internal Server Error` - Server error

## Public Endpoints

### Health Check

Check if Ground Control is running.

**Endpoint:** `GET /health`

**Response:**
```
200 OK
```

**Example:**
```bash
curl http://localhost:9090/health
```

### Ping

Simple ping endpoint.

**Endpoint:** `GET /ping`

**Response:**
```
200 OK
```

### Login

Authenticate and get a session token.

**Endpoint:** `POST /login`

**Request Body:**
```json
{
  "username": "admin",
  "password": "SecurePass123"
}
```

**Response:**
```json
{
  "token": "session-token-here",
  "expires_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST http://localhost:9090/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "SecurePass123"
  }'
```

**Notes:**
- Account is locked after 5 failed login attempts
- Lockout duration is configurable (default: 15 minutes)
- Session duration is configurable (default: 24 hours)

### Get Satellite Configuration (ZTR)

Get satellite configuration using satellite token. This endpoint is used by satellites during registration.

**Endpoint:** `GET /satellites/ztr/{token}`

**Path Parameters:**
- `token` (string, required): Satellite token

**Response:**
```json
{
  "state": "https://harbor.example.com/satellite/satellite-state/satellite-name/state:latest",
  "auth": {
    "url": "https://harbor.example.com",
    "username": "robot_account_name",
    "password": "robot_account_secret"
  }
}
```

**Example:**
```bash
curl http://localhost:9090/satellites/ztr/satellite-token-here
```

### Satellite Heartbeat

Satellite heartbeat endpoint. Used by satellites to report status.

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

**Response:**
```
200 OK
```

**Example:**
```bash
curl -X POST http://localhost:9090/satellites/sync \
  -H "Content-Type: application/json" \
  -d '{
    "name": "satellite_1",
    "activity": "idle",
    "state_report_interval": "@every 00h01m00s",
    "memory_used_bytes": 1073741824,
    "storage_used_bytes": 2147483648,
    "cpu_percent": 25.5,
    "request_created_time": "2024-01-01T12:00:00Z",
    "last_sync_duration_ms": 1500,
    "image_count": 42
  }'
```

## Protected Endpoints

All protected endpoints require a Bearer token in the `Authorization` header.

### Authentication Endpoints

#### Logout

Invalidate the current session token.

**Endpoint:** `POST /logout`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```
204 No Content
```

**Example:**
```bash
curl -X POST http://localhost:9090/logout \
  -H "Authorization: Bearer $TOKEN"
```

### User Management

#### List Users

List all users (excluding system_admin).

**Endpoint:** `GET /users`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 1,
    "username": "user1",
    "role": "admin",
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/users \
  -H "Authorization: Bearer $TOKEN"
```

#### Get User

Get a specific user by username.

**Endpoint:** `GET /users/{username}`

**Path Parameters:**
- `username` (string, required): Username

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{
  "id": 1,
  "username": "user1",
  "role": "admin",
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl http://localhost:9090/users/user1 \
  -H "Authorization: Bearer $TOKEN"
```

#### Create User

Create a new admin user. Requires `system_admin` role.

**Endpoint:** `POST /users`

**Headers:**
- `Authorization: Bearer <token>` (system_admin required)

**Request Body:**
```json
{
  "username": "newuser",
  "password": "SecurePass123"
}
```

**Response:**
```json
{
  "id": 2,
  "username": "newuser",
  "role": "admin",
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST http://localhost:9090/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "newuser",
    "password": "SecurePass123"
  }'
```

**Notes:**
- Username "admin" is reserved
- Password must meet password policy requirements

#### Delete User

Delete a user. Requires `system_admin` role.

**Endpoint:** `DELETE /users/{username}`

**Path Parameters:**
- `username` (string, required): Username

**Headers:**
- `Authorization: Bearer <token>` (system_admin required)

**Response:**
```
204 No Content
```

**Example:**
```bash
curl -X DELETE http://localhost:9090/users/user1 \
  -H "Authorization: Bearer $TOKEN"
```

**Notes:**
- Cannot delete yourself
- Cannot delete "admin" user

#### Change Own Password

Change the current user's password.

**Endpoint:** `PATCH /users/password`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "current_password": "OldPass123",
  "new_password": "NewPass123"
}
```

**Response:**
```
204 No Content
```

**Example:**
```bash
curl -X PATCH http://localhost:9090/users/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "current_password": "OldPass123",
    "new_password": "NewPass123"
  }'
```

**Notes:**
- Invalidates all user sessions after password change

#### Change User Password

Change any user's password. Requires `system_admin` role.

**Endpoint:** `PATCH /users/{username}/password`

**Path Parameters:**
- `username` (string, required): Username

**Headers:**
- `Authorization: Bearer <token>` (system_admin required)

**Request Body:**
```json
{
  "new_password": "NewPass123"
}
```

**Response:**
```
204 No Content
```

**Example:**
```bash
curl -X PATCH http://localhost:9090/users/user1/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "new_password": "NewPass123"
  }'
```

**Notes:**
- Invalidates all user sessions after password change

### Groups

#### List Groups

List all groups.

**Endpoint:** `GET /groups`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 1,
    "group_name": "group1",
    "registry_url": "http://localhost:8080",
    "projects": ["project1", "project2"],
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/groups \
  -H "Authorization: Bearer $TOKEN"
```

#### Get Group

Get a specific group by name.

**Endpoint:** `GET /groups/{group}`

**Path Parameters:**
- `group` (string, required): Group name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{
  "id": 1,
  "group_name": "group1",
  "registry_url": "http://localhost:8080",
  "projects": ["project1", "project2"],
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl http://localhost:9090/groups/group1 \
  -H "Authorization: Bearer $TOKEN"
```

#### Create/Update Group

Create or update a group with artifacts.

**Endpoint:** `POST /groups/sync`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "group": "group1",
  "registry": "http://localhost:8080",
  "artifacts": [
    {
      "repository": "library/alpine",
      "tag": ["latest"],
      "type": "docker",
      "digest": "sha256:5a6ee6c36824d527a0fe91a2a7c160c2e286bbeae46cd931c337ac769f1bd930",
      "deleted": false
    }
  ]
}
```

**Response:**
```json
{
  "id": 1,
  "group_name": "group1",
  "registry_url": "http://localhost:8080",
  "projects": ["library"],
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X POST http://localhost:9090/groups/sync \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "group": "group1",
    "registry": "http://localhost:8080",
    "artifacts": [
      {
        "repository": "library/alpine",
        "tag": ["latest"],
        "type": "docker",
        "digest": "sha256:5a6ee6c36824d527a0fe91a2a7c160c2e286bbeae46cd931c337ac769f1bd930",
        "deleted": false
      }
    ]
  }'
```

**Notes:**
- Creates group if it doesn't exist
- Updates group if it exists
- Updates robot account permissions for all satellites in the group

#### List Satellites in Group

List all satellites attached to a specific group.

**Endpoint:** `GET /groups/{group}/satellites`

**Path Parameters:**
- `group` (string, required): Group name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 1,
    "name": "satellite_1",
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/groups/group1/satellites \
  -H "Authorization: Bearer $TOKEN"
```

#### Add Satellite to Group

Add a satellite to a group.

**Endpoint:** `POST /groups/satellite`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "satellite": "satellite_1",
  "group": "group1"
}
```

**Response:**
```json
{
  "message": "Satellite successfully added to group"
}
```

**Example:**
```bash
curl -X POST http://localhost:9090/groups/satellite \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "satellite": "satellite_1",
    "group": "group1"
  }'
```

**Notes:**
- Updates robot account permissions
- Updates satellite state artifact

#### Remove Satellite from Group

Remove a satellite from a group.

**Endpoint:** `DELETE /groups/satellite`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "satellite": "satellite_1",
  "group": "group1"
}
```

**Response:**
```json
{}
```

**Example:**
```bash
curl -X DELETE http://localhost:9090/groups/satellite \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "satellite": "satellite_1",
    "group": "group1"
  }'
```

**Notes:**
- Updates robot account permissions
- Updates satellite state artifact

### Configs

#### List Configs

List all configurations.

**Endpoint:** `GET /configs`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 1,
    "config_name": "config1",
    "registry_url": "http://localhost:8080",
    "config": {...},
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/configs \
  -H "Authorization: Bearer $TOKEN"
```

#### Get Config

Get a specific configuration by name.

**Endpoint:** `GET /configs/{config}`

**Path Parameters:**
- `config` (string, required): Config name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{
  "id": 1,
  "config_name": "config1",
  "registry_url": "http://localhost:8080",
  "config": {
    "state_config": {...},
    "app_config": {...},
    "zot_config": {...}
  },
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl http://localhost:9090/configs/config1 \
  -H "Authorization: Bearer $TOKEN"
```

#### Create Config

Create a new configuration.

**Endpoint:** `POST /configs`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "config_name": "config1",
  "config": {
    "state_config": {},
    "app_config": {
      "ground_control_url": "http://127.0.0.1:9090",
      "log_level": "info"
    },
    "zot_config": {
      "distSpecVersion": "1.1.0",
      "storage": {
        "rootDirectory": "./zot"
      },
      "http": {
        "address": "0.0.0.0",
        "port": "8585"
      }
    }
  }
}
```

**Response:**
```
201 Created
```

**Example:**
```bash
curl -X POST http://localhost:9090/configs \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config_name": "config1",
    "config": {...}
  }'
```

**Notes:**
- Config name must be unique
- Config is stored as an OCI artifact in Harbor

#### Update Config

Update an existing configuration using JSON merge patch.

**Endpoint:** `PATCH /configs/{config}`

**Path Parameters:**
- `config` (string, required): Config name

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "app_config": {
    "log_level": "debug"
  }
}
```

**Response:**
```json
{
  "id": 1,
  "config_name": "config1",
  "registry_url": "http://localhost:8080",
  "config": {...},
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl -X PATCH http://localhost:9090/configs/config1 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "app_config": {
      "log_level": "debug"
    }
  }'
```

**Notes:**
- Uses JSON merge patch semantics
- Updates config artifact in Harbor

#### Delete Config

Delete a configuration. Cannot delete configs that are in use by satellites.

**Endpoint:** `DELETE /configs/{config}`

**Path Parameters:**
- `config` (string, required): Config name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{}
```

**Example:**
```bash
curl -X DELETE http://localhost:9090/configs/config1 \
  -H "Authorization: Bearer $TOKEN"
```

**Notes:**
- Returns error if config is in use by any satellite

#### Set Satellite Config

Assign a configuration to a satellite.

**Endpoint:** `POST /configs/satellite`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "satellite": "satellite_1",
  "config_name": "config1"
}
```

**Response:**
```json
{}
```

**Example:**
```bash
curl -X POST http://localhost:9090/configs/satellite \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "satellite": "satellite_1",
    "config_name": "config1"
  }'
```

**Notes:**
- Updates satellite state artifact

### Satellites

#### List Satellites

List all satellites.

**Endpoint:** `GET /satellites`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 1,
    "name": "satellite_1",
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/satellites \
  -H "Authorization: Bearer $TOKEN"
```

#### Get Satellite

Get a specific satellite by name.

**Endpoint:** `GET /satellites/{satellite}`

**Path Parameters:**
- `satellite` (string, required): Satellite name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{
  "id": 1,
  "name": "satellite_1",
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl http://localhost:9090/satellites/satellite_1 \
  -H "Authorization: Bearer $TOKEN"
```

#### Register Satellite

Register a new satellite.

**Endpoint:** `POST /satellites`

**Headers:**
- `Authorization: Bearer <token>`

**Request Body:**
```json
{
  "name": "satellite_1",
  "groups": ["group1"],
  "config_name": "config1"
}
```

**Response:**
```json
{
  "token": "satellite-token-here"
}
```

**Example:**
```bash
curl -X POST http://localhost:9090/satellites \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
  }'
```

**Notes:**
- Creates robot account in Harbor
- Creates satellite state artifact
- Returns satellite token (save this for satellite startup)
- Robot account name must be unique

#### Delete Satellite

Delete a satellite.

**Endpoint:** `DELETE /satellites/{satellite}`

**Path Parameters:**
- `satellite` (string, required): Satellite name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{}
```

**Example:**
```bash
curl -X DELETE http://localhost:9090/satellites/satellite_1 \
  -H "Authorization: Bearer $TOKEN"
```

**Notes:**
- Deletes robot account in Harbor
- Deletes satellite state artifact

#### Get Satellite Status

Get the latest status report from a satellite.

**Endpoint:** `GET /satellites/{satellite}/status`

**Path Parameters:**
- `satellite` (string, required): Satellite name

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
{
  "id": 1,
  "satellite_id": 1,
  "activity": "idle",
  "latest_state_digest": "sha256:abc123...",
  "latest_config_digest": "sha256:def456...",
  "cpu_percent": "25.50",
  "memory_used_bytes": 1073741824,
  "storage_used_bytes": 2147483648,
  "last_sync_duration_ms": 1500,
  "image_count": 42,
  "reported_at": "2024-01-01T12:00:00Z"
}
```

**Example:**
```bash
curl http://localhost:9090/satellites/satellite_1/status \
  -H "Authorization: Bearer $TOKEN"
```

#### Get Active Satellites

Get all satellites that have sent a heartbeat recently.

**Endpoint:** `GET /satellites/active`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 1,
    "name": "satellite_1",
    "created_at": "2024-01-01T12:00:00Z",
    "last_seen": "2024-01-01T13:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/satellites/active \
  -H "Authorization: Bearer $TOKEN"
```

#### Get Stale Satellites

Get all satellites that haven't sent a heartbeat recently.

**Endpoint:** `GET /satellites/stale`

**Headers:**
- `Authorization: Bearer <token>`

**Response:**
```json
[
  {
    "id": 2,
    "name": "satellite_2",
    "created_at": "2024-01-01T12:00:00Z",
    "last_seen": "2024-01-01T10:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:9090/satellites/stale \
  -H "Authorization: Bearer $TOKEN"
```

## Related Documentation

- [Getting Started Guide](getting-started.md) - Initial setup instructions
- [Configuration Reference](configuration.md) - Configuration options
- [Architecture Documentation](architecture/overview.md) - System architecture
