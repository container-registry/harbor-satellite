# API Reference

This document provides a comprehensive reference for the Ground Control REST API endpoints used to manage Harbor Satellite deployments.

## Authentication

Most API endpoints require authentication using Bearer tokens. Obtain a token by logging in:

```bash
curl -X POST http://localhost:9090/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "SecurePass123"
  }'
```

Include the token in requests:
```
Authorization: Bearer <token>
```

## Public Endpoints

### Health Check

**GET** `/health`

Returns the health status of Ground Control.

**Response:**
```json
{
  "status": "ok"
}
```

### Ping

**GET** `/ping`

Simple ping endpoint for connectivity testing.

**Response:**
```text
pong
```

### Login

**POST** `/login`

Authenticate and obtain a session token.

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response:**
```json
{
  "token": "string",
  "expires_at": "2024-01-01T00:00:00Z"
}
```

### Satellite Sync (Heartbeat)

**POST** `/satellites/sync`

Satellite heartbeat endpoint for status updates. Uses satellite token authentication.

**Headers:**
```
Authorization: Bearer <satellite-token>
```

**Request Body:**
```json
{
  "name": "satellite-name",
  "status": "active|inactive",
  "last_seen": "2024-01-01T00:00:00Z"
}
```

## Protected Endpoints

All protected endpoints require `Authorization: Bearer <token>` header.

### Authentication

#### Logout

**POST** `/logout`

Invalidate the current session token.

### Users

#### List Users

**GET** `/users`

List all users. Requires authentication.

**Response:**
```json
[
  {
    "username": "string",
    "role": "system_admin|user",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

#### Get User

**GET** `/users/{username}`

Get details for a specific user.

**Response:**
```json
{
  "username": "string",
  "role": "system_admin|user",
  "created_at": "2024-01-01T00:00:00Z",
  "last_login": "2024-01-01T00:00:00Z"
}
```

#### Create User

**POST** `/users`

Create a new user. Requires system_admin role.

**Request Body:**
```json
{
  "username": "string",
  "password": "string",
  "role": "system_admin|user"
}
```

#### Change Own Password

**PATCH** `/users/password`

Change the current user's password.

**Request Body:**
```json
{
  "current_password": "string",
  "new_password": "string"
}
```

#### Change User Password

**PATCH** `/users/{username}/password`

Change another user's password. Requires system_admin role.

**Request Body:**
```json
{
  "new_password": "string"
}
```

#### Delete User

**DELETE** `/users/{username}`

Delete a user. Requires system_admin role.

### Groups

#### List Groups

**GET** `/groups`

List all artifact groups.

**Response:**
```json
[
  {
    "name": "string",
    "registry": "string",
    "artifacts": [
      {
        "repository": "string",
        "tag": ["string"],
        "type": "docker|oci",
        "digest": "string",
        "deleted": false
      }
    ],
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

#### Get Group

**GET** `/groups/{group}`

Get details for a specific group.

#### Sync Group

**POST** `/groups/sync`

Create or update an artifact group.

**Request Body:**
```json
{
  "group": "string",
  "registry": "string",
  "artifacts": [
    {
      "repository": "string",
      "tag": ["string"],
      "type": "docker",
      "digest": "string",
      "deleted": false
    }
  ]
}
```

#### List Satellites in Group

**GET** `/groups/{group}/satellites`

List satellites assigned to a group.

**Response:**
```json
[
  {
    "name": "string",
    "status": "active|inactive",
    "last_seen": "2024-01-01T00:00:00Z"
  }
]
```

#### Add Satellite to Group

**POST** `/groups/satellite`

Add a satellite to a group.

**Request Body:**
```json
{
  "satellite": "string",
  "group": "string"
}
```

#### Remove Satellite from Group

**DELETE** `/groups/satellite`

Remove a satellite from a group.

**Request Body:**
```json
{
  "satellite": "string",
  "group": "string"
}
```

### Configurations

#### List Configurations

**GET** `/configs`

List all satellite configurations.

**Response:**
```json
[
  {
    "name": "string",
    "config": {
      "state_config": {},
      "app_config": {},
      "zot_config": {}
    },
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

#### Get Configuration

**GET** `/configs/{config}`

Get a specific configuration.

#### Create Configuration

**POST** `/configs`

Create a new satellite configuration.

**Request Body:**
```json
{
  "config_name": "string",
  "config": {
    "state_config": {
      "auth": {
        "url": "string",
        "username": "string",
        "password": "string"
      }
    },
    "app_config": {
      "ground_control_url": "string",
      "log_level": "string",
      "use_unsecure": false,
      "state_replication_interval": "string",
      "register_satellite_interval": "string",
      "local_registry": {
        "url": "string"
      },
      "heartbeat_interval": "string"
    },
    "zot_config": {}
  }
}
```

#### Update Configuration

**PATCH** `/configs/{config}`

Update an existing configuration.

#### Delete Configuration

**DELETE** `/configs/{config}`

Delete a configuration.

#### Set Satellite Configuration

**POST** `/configs/satellite`

Assign a configuration to a satellite.

**Request Body:**
```json
{
  "satellite": "string",
  "config": "string"
}
```

### Satellites

#### List Satellites

**GET** `/satellites`

List all registered satellites.

**Response:**
```json
[
  {
    "name": "string",
    "groups": ["string"],
    "config_name": "string",
    "status": "active|inactive",
    "last_seen": "2024-01-01T00:00:00Z",
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

#### Register Satellite

**POST** `/satellites`

Register a new satellite.

**Request Body:**
```json
{
  "name": "string",
  "groups": ["string"],
  "config_name": "string"
}
```

**Response:**
```json
{
  "name": "string",
  "token": "string",
  "groups": ["string"],
  "config_name": "string"
}
```

#### Get Satellite

**GET** `/satellites/{satellite}`

Get details for a specific satellite.

#### Delete Satellite

**DELETE** `/satellites/{satellite}`

Delete a satellite.

#### Get Satellite Status

**GET** `/satellites/{satellite}/status`

Get the current status of a satellite.

**Response:**
```json
{
  "name": "string",
  "status": "active|inactive",
  "last_seen": "2024-01-01T00:00:00Z",
  "groups": ["string"],
  "config_name": "string"
}
```

#### Get Active Satellites

**GET** `/satellites/active`

List all active satellites.

#### Get Stale Satellites

**GET** `/satellites/stale`

List satellites that haven't checked in recently.

## Error Responses

All endpoints return standard HTTP status codes. Error responses include:

```json
{
  "error": "string",
  "message": "string",
  "code": 400
}
```

Common error codes:
- `400` - Bad Request
- `401` - Unauthorized
- `403` - Forbidden
- `404` - Not Found
- `409` - Conflict
- `500` - Internal Server Error

## Rate Limiting

API endpoints are rate limited. Exceeding limits returns `429 Too Many Requests`.

## Content Types

- Request bodies: `application/json`
- Response bodies: `application/json`
- Error responses: `application/json`</content>
<parameter name="filePath">/home/anurag2004/harbor-satellite/docs/api-reference.md