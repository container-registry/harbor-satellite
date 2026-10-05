---
status: accepted
---

# Ground Control Authentication

## Context and Problem Statement

Ground Control manages satellites via HTTP API. Without authentication, any client can modify
satellite configurations and user data. This feature adds session-based authentication.

## Decision Outcome

Chosen option: "Session-based authentication with bearer tokens" for simplicity, stateful session
management, and brute-force protection without requiring external identity providers.

## Implementation Summary

### Database Schema (3 tables)

* `users`: username, argon2id password hash, role (system_admin|admin)
* `sessions`: token, user_id, expires_at
* `login_attempts`: failed count, locked_until (for account lockout)

### Authentication Methods

1. Bearer Token: `Authorization: Bearer <session_token>`
2. Basic Auth: `Authorization: Basic <base64(username:password)>`

### Password Security

* Argon2id hashing (OWASP 2024 parameters: 19MiB memory, 2 iterations)
* Configurable policy via env vars: `PASSWORD_MIN_LENGTH`, `PASSWORD_MAX_LENGTH`,
  `PASSWORD_REQUIRE_UPPERCASE/LOWERCASE/NUMBER/SPECIAL`

### Account Lockout

* 5 failed attempts triggers lockout for `LOCKOUT_DURATION` (Go duration, default: `5m`)

### User Roles

* `system_admin`: Full access including user management, bootstrapped via `ADMIN_PASSWORD` env
* `admin`: Standard access to satellites/groups/configs, no user management

### API Routes

Public:
* `GET /ping`, `GET /health`, `POST /login`, `POST /satellites/ztr` (rate limited)

Token-based ZTR sends the single-use token in the JSON request body as
`{"token":"<32-character token>"}`; the token is not part of the URL.

Satellite-authenticated (robot account basic auth or SPIFFE mTLS, added in #500):
* `POST /satellites/sync`, `GET /satellites/spiffe-ztr` (SPIFFE only)

Protected (all authenticated users):
* `POST /api/logout`, `GET /api/users`, `GET /api/users/{username}`, `PATCH /api/users/password`

Protected (system_admin only):
* `POST /api/users`, `DELETE /api/users/{username}`, `PATCH /api/users/{username}/password`
* `DELETE /api/groups/{group}`, `POST /api/satellites/register`, `/api/spire/*`

All other `/api/groups/*`, `/api/configs/*`, `/api/satellites/*` management endpoints require authentication.

### Session Management

* Configurable duration via `SESSION_DURATION` (Go duration, default: `24h`)
* Password changes invalidate all user sessions
* User deletion cascades to session deletion

### Bootstrap Process

On startup: creates `admin` user from `ADMIN_PASSWORD` env if not exists.

## Configuration

Required for initial setup:
```
ADMIN_PASSWORD=<strong-password>
```

Optional:
```
SESSION_DURATION=24h
LOCKOUT_DURATION=5m
PASSWORD_MIN_LENGTH=8
PASSWORD_REQUIRE_SPECIAL=false
```

## Consequences

* Good: All administrative endpoints protected
* Good: Flexible auth methods (token or basic)
* Good: Brute-force protection via lockout
* Good: Configurable security policies
* Neutral: Requires initial `ADMIN_PASSWORD` setup
