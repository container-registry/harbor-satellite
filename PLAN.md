# Implementation Plan: Dual Auth System for Harbor Satellite

## Overview

This plan merges the `plan-spiffe` branch (SPIFFE/SPIRE auth) with functionality from `main` branch (user management, ZTR token flow) to create a complete authentication system.

## Current State

### Already Implemented in plan-spiffe

**Satellite SPIFFE Client** (`internal/spiffe/client.go`):
- mTLS client with X509Source from SPIRE agent
- `GetTLSConfig()`, `CreateHTTPClient()` for SPIFFE-authenticated requests
- `WaitForSVID()`, certificate extraction helpers

**Ground Control SPIFFE** (`ground-control/internal/spiffe/`):
- `Provider` interface with `SidecarProvider` (SPIRE agent) and `StaticProvider` (pre-loaded certs)
- `SatelliteAuthorizer`, `RegionAuthorizer`, `PatternAuthorizer` for SPIFFE ID validation
- `RequireSPIFFEAuth` middleware, `DualAuthMiddleware` (token + SPIFFE)
- Join token generation handler, SPIRE status endpoint

**Device Identity** (`internal/identity/`):
- Linux device fingerprinting (machine-id, MAC address, disk serial)
- Used for device-bound encryption keys

**Crypto** (`internal/crypto/`):
- AES encryption provider for secure config storage

**Rate Limiting** (`ground-control/internal/middleware/`):
- IP-based rate limiter with configurable limits

### Missing from plan-spiffe (exists in main)

**User Management**:
- `internal/auth/` - Password hashing (Argon2id), policy
- `internal/server/user_handlers.go` - User CRUD
- `internal/server/auth_handlers.go` - Login/logout
- `internal/server/middleware.go` - User auth middleware
- Database: users, sessions, login_attempts tables

**Satellite Status**:
- `internal/database/satellite_status.sql.go`
- Status/sync endpoints, heartbeat tracking
- Active/stale satellite queries

### Future Work (Not in this plan)
- TPM integration for hardware-bound identity

## Architecture

### Authentication Paths

| Path | Auth Method | Description |
|------|-------------|-------------|
| GC-Human | User/password/session | Admin dashboard, management APIs |
| Satellite-GC (ZTR) | Token-based | Single-use bootstrap token |
| Satellite-GC (SPIFFE) | mTLS with X.509 SVID | Certificate-based identity |
| Satellite-GC (sync) | Robot credentials | Ongoing heartbeat after bootstrap |

### Route Structure

```
/ping, /health                          - Public
/login                                  - Public (rate limited)

/api/*                                  - Human routes (user auth required)
  /api/logout
  /api/users/*
  /api/groups/*
  /api/configs/*
  /api/satellites/*                     - Management only

/satellites/*                           - Satellite routes (robot creds OR SPIFFE)
  /satellites/ztr/{token}               - Token-based ZTR (rate limited)
  /satellites/spiffe-ztr                - SPIFFE-based ZTR (rate limited)
  /satellites/sync                      - Heartbeat with metrics
  /satellites/{satellite}/join-token    - SPIRE join token

/spire/status                           - SPIRE status (admin only under /api)
```

### Build Options

| Component | Tag | Description |
|-----------|-----|-------------|
| Satellite | (default) | Full binary with SPIFFE, crypto, identity |
| Satellite | `nospiffe` | Minimal binary, ZTR-only |
| Ground Control | (default) | Full with SPIFFE support |
| Ground Control | `nospiffe` | Without SPIFFE dependencies |

## Configuration

### From main (already implemented, copy as-is)

| Setting | Description |
|---------|-------------|
| Session duration | User session lifetime |
| Lockout policy | Failed login attempts and lockout duration |
| Admin bootstrap | System admin creation on first run |
| Stale satellite threshold | Time before satellite marked stale |

### From plan-spiffe (already implemented)

| Setting | Value | Source |
|---------|-------|--------|
| SPIFFE trust domain | harbor-satellite.local | Env: SPIFFE_TRUST_DOMAIN |
| SPIFFE auto-register | Yes | Auto-create satellite from SPIFFE ID |
| Rate limiting | Auth routes | Existing middleware |

### New/Modified

| Setting | Description |
|---------|-------------|
| Sync payload | CPU, memory, storage metrics |
| Sync authentication | Robot credentials from ZTR |

## Implementation Phases

### Phase 1: Database Migrations

Copy from `../main/ground-control/` and renumber (plan-spiffe has 008_token_expiry.sql):

| Source (main) | Target (plan-spiffe) |
|---------------|---------------------|
| sql/schema/008_satellite_status.sql | sql/schema/009_satellite_status.sql |
| sql/schema/009_satellites_last_seen.sql | sql/schema/010_satellites_last_seen.sql |
| sql/schema/011_users.sql | sql/schema/011_users.sql |
| sql/schema/012_sessions.sql | sql/schema/012_sessions.sql |
| sql/schema/013_login_attempts.sql | sql/schema/013_login_attempts.sql |

Copy query files:
- `sql/queries/users.sql`
- `sql/queries/sessions.sql`
- `sql/queries/login_attempts.sql`
- `sql/queries/satellite_status.sql`

Regenerate: `cd ground-control && sqlc generate`

### Phase 2: Auth Package

Copy from `../main/ground-control/internal/auth/`:
- `password.go` - Argon2id hashing
- `policy.go` - Password policy enforcement

No modifications needed.

### Phase 3: User Auth Middleware

Copy `../main/ground-control/internal/server/middleware.go` as-is.

Already implemented in main:
- Lockout duration configuration
- Max login attempts configuration
- Auth failure logging

### Phase 4: Handlers

**Copy from main** (no changes needed):
- `ground-control/internal/server/user_handlers.go`
- `ground-control/internal/server/auth_handlers.go`
- `ground-control/internal/server/bootstrap.go` (ADMIN_PASSWORD handling already implemented)
- `ground-control/internal/server/cleanup.go`

**Add to satellite_handlers.go** (copy from main and adapt):

```go
// syncHandler - Heartbeat with metrics
// Dual auth: check SPIFFE context first, then validate robot credentials
func (s *Server) syncHandler(w http.ResponseWriter, r *http.Request) {
    var satelliteName string

    // Check SPIFFE identity first
    if name, ok := spiffe.GetSatelliteName(r.Context()); ok {
        satelliteName = name
    } else {
        // Validate robot credentials from request
        // ...
    }
    // Record status with metrics (CPU, memory, storage)
}

// getSatelliteStatusHandler
// getActiveSatellitesHandler
// getStaleSatellitesHandler
```

**Sync Request Payload**:
```go
type SatelliteStatusParams struct {
    Name              string  `json:"name"`
    RobotUsername     string  `json:"robot_username"`
    RobotPassword     string  `json:"robot_password"`
    CPUUsage          float64 `json:"cpu_usage"`
    MemoryUsage       float64 `json:"memory_usage"`
    StorageUsage      float64 `json:"storage_usage"`
    HeartbeatInterval int     `json:"heartbeat_interval"`
}
```

### Phase 5: Routes Restructuring

Modify `ground-control/internal/server/routes.go`:

```go
func (s *Server) RegisterRoutes() http.Handler {
    r := mux.NewRouter()

    // Public
    r.HandleFunc("/ping", s.Ping).Methods("GET")
    r.HandleFunc("/health", s.healthHandler).Methods("GET")

    // Login (rate limited)
    loginRouter := r.PathPrefix("/login").Subrouter()
    loginRouter.Use(middleware.RateLimitMiddleware(s.rateLimiter))
    loginRouter.HandleFunc("", s.loginHandler).Methods("POST")

    // Human API routes (user auth required)
    api := r.PathPrefix("/api").Subrouter()
    api.Use(s.AuthMiddleware)

    // Users
    api.HandleFunc("/logout", s.logoutHandler).Methods("POST")
    api.HandleFunc("/users", s.listUsersHandler).Methods("GET")
    api.HandleFunc("/users/password", s.changeOwnPasswordHandler).Methods("PATCH")
    api.HandleFunc("/users/{username}", s.getUserHandler).Methods("GET")
    api.HandleFunc("/users", s.RequireRole(roleSystemAdmin, s.createUserHandler)).Methods("POST")
    api.HandleFunc("/users/{username}", s.RequireRole(roleSystemAdmin, s.deleteUserHandler)).Methods("DELETE")
    api.HandleFunc("/users/{username}/password", s.RequireRole(roleSystemAdmin, s.changeUserPasswordHandler)).Methods("PATCH")

    // Groups
    api.HandleFunc("/groups", s.listGroupHandler).Methods("GET")
    api.HandleFunc("/groups/sync", s.groupsSyncHandler).Methods("POST")
    api.HandleFunc("/groups/{group}", s.getGroupHandler).Methods("GET")
    api.HandleFunc("/groups/{group}/satellites", s.groupSatelliteHandler).Methods("GET")
    api.HandleFunc("/groups/satellite", s.addSatelliteToGroup).Methods("POST")
    api.HandleFunc("/groups/satellite", s.removeSatelliteFromGroup).Methods("DELETE")

    // Configs
    api.HandleFunc("/configs", s.listConfigsHandler).Methods("GET")
    api.HandleFunc("/configs", s.createConfigHandler).Methods("POST")
    api.HandleFunc("/configs/{config}", s.updateConfigHandler).Methods("PATCH")
    api.HandleFunc("/configs/{config}", s.getConfigHandler).Methods("GET")
    api.HandleFunc("/configs/{config}", s.deleteConfigHandler).Methods("DELETE")
    api.HandleFunc("/configs/satellite", s.setSatelliteConfig).Methods("POST")

    // Satellite management (human only)
    api.HandleFunc("/satellites", s.listSatelliteHandler).Methods("GET")
    api.HandleFunc("/satellites", s.registerSatelliteHandler).Methods("POST")
    api.HandleFunc("/satellites/active", s.getActiveSatellitesHandler).Methods("GET")
    api.HandleFunc("/satellites/stale", s.getStaleSatellitesHandler).Methods("GET")
    api.HandleFunc("/satellites/{satellite}", s.GetSatelliteByName).Methods("GET")
    api.HandleFunc("/satellites/{satellite}", s.DeleteSatelliteByName).Methods("DELETE")
    api.HandleFunc("/satellites/{satellite}/status", s.getSatelliteStatusHandler).Methods("GET")

    // SPIRE management (admin only)
    api.HandleFunc("/spire/status", s.RequireRole(roleSystemAdmin, s.spireStatusHandler)).Methods("GET")

    // Satellite routes (robot creds or SPIFFE)
    satellites := r.PathPrefix("/satellites").Subrouter()

    // Token-based ZTR (rate limited)
    ztr := satellites.PathPrefix("/ztr").Subrouter()
    ztr.Use(middleware.RateLimitMiddleware(s.rateLimiter))
    ztr.HandleFunc("/{token}", s.ztrHandler).Methods("GET")

    // SPIFFE-based ZTR (rate limited)
    spiffeZtr := satellites.PathPrefix("/spiffe-ztr").Subrouter()
    spiffeZtr.Use(spiffe.RequireSPIFFEAuth)
    spiffeZtr.Use(middleware.RateLimitMiddleware(s.rateLimiter))
    spiffeZtr.HandleFunc("", s.spiffeZtrHandler).Methods("GET")

    // Sync (both auth modes)
    satellites.HandleFunc("/sync", s.syncHandler).Methods("POST")

    // Join token (satellite requests)
    satellites.HandleFunc("/{satellite}/join-token", s.generateJoinTokenHandler).Methods("POST")

    return r
}
```

### Phase 6: Server Struct

Modify `ground-control/internal/server/server.go`:

```go
type Server struct {
    port            int
    db              *sql.DB
    dbQueries       *database.Queries

    // User auth (restore from main)
    passwordPolicy   auth.PasswordPolicy
    sessionDuration  time.Duration      // 24h
    lockoutDuration  time.Duration      // 5m
    maxLoginAttempts int                // 10

    // SPIFFE (existing in plan-spiffe)
    spiffeProvider  spiffe.Provider    // nil if nospiffe build
    rateLimiter     *middleware.RateLimiter

    // Satellite status
    staleThreshold  time.Duration      // 1h
}
```

### Phase 7: Build Tags - Satellite

Create stub files for `nospiffe` build:

**internal/spiffe/client_stub.go**:
```go
//go:build nospiffe

package spiffe

import "errors"

var ErrSPIFFENotAvailable = errors.New("SPIFFE not available in this build")

type Client struct{}
type Config struct {
    Enabled bool
}

func DefaultConfig() Config { return Config{Enabled: false} }
func NewClient(cfg Config) (*Client, error) { return nil, ErrSPIFFENotAvailable }
func (c *Client) Connect(ctx context.Context) error { return ErrSPIFFENotAvailable }
func (c *Client) GetSVID() (interface{}, error) { return nil, ErrSPIFFENotAvailable }
func (c *Client) GetTLSConfig() (interface{}, error) { return nil, ErrSPIFFENotAvailable }
func (c *Client) CreateHTTPClient() (interface{}, error) { return nil, ErrSPIFFENotAvailable }
func (c *Client) Close() error { return nil }
```

**internal/crypto/provider_stub.go**:
```go
//go:build nospiffe

package crypto

import "errors"

var ErrCryptoNotAvailable = errors.New("crypto not available in minimal build")

type Provider interface {
    Encrypt(data []byte) ([]byte, error)
    Decrypt(data []byte) ([]byte, error)
}

type NoOpProvider struct{}

func NewNoOpProvider() *NoOpProvider { return &NoOpProvider{} }
func (p *NoOpProvider) Encrypt(data []byte) ([]byte, error) { return data, nil }
func (p *NoOpProvider) Decrypt(data []byte) ([]byte, error) { return data, nil }
```

**internal/identity/device_stub.go**:
```go
//go:build nospiffe

package identity

type NoOpIdentity struct{}

func NewNoOpIdentity() *NoOpIdentity { return &NoOpIdentity{} }
func (d *NoOpIdentity) GetFingerprint() (string, error) { return "", ErrComponentUnavailable }
func (d *NoOpIdentity) GetMACAddress() (string, error) { return "", ErrComponentUnavailable }
func (d *NoOpIdentity) GetCPUID() (string, error) { return "", ErrComponentUnavailable }
func (d *NoOpIdentity) GetBootID() (string, error) { return "", ErrComponentUnavailable }
func (d *NoOpIdentity) GetDiskSerial() (string, error) { return "", ErrComponentUnavailable }
func (d *NoOpIdentity) GetMachineID() (string, error) { return "", ErrComponentUnavailable }
```

Add build tag to existing files:
- `internal/spiffe/client.go` - add `//go:build !nospiffe`
- `internal/crypto/aes_provider.go` - add `//go:build !nospiffe`
- `internal/identity/device_linux.go` - add `//go:build linux && !nospiffe`

### Phase 8: Build Tags - Ground Control

**ground-control/internal/spiffe/provider_stub.go**:
```go
//go:build nospiffe

package spiffe

import (
    "context"
    "crypto/tls"
    "errors"
)

var ErrSPIFFENotAvailable = errors.New("SPIFFE not available in this build")

type Provider interface {
    GetX509Source(ctx context.Context) (interface{}, error)
    GetTLSConfig(ctx context.Context, authorizer interface{}) (*tls.Config, error)
    GetTrustDomain() interface{}
    Close() error
}

type Config struct {
    Enabled bool
}

func LoadConfig() *Config { return &Config{Enabled: false} }
func NewProvider(cfg *Config) (Provider, error) { return nil, ErrSPIFFENotAvailable }
```

**ground-control/internal/spiffe/middleware_stub.go**:
```go
//go:build nospiffe

package spiffe

import (
    "context"
    "net/http"
)

func AuthMiddleware(next http.Handler) http.Handler { return next }
func RequireSPIFFEAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "SPIFFE not available", http.StatusNotImplemented)
    })
}
func GetSPIFFEID(ctx context.Context) (interface{}, bool) { return nil, false }
func GetSatelliteName(ctx context.Context) (string, bool) { return "", false }
func GetRegion(ctx context.Context) (string, bool) { return "", false }
```

Add build tag to existing files:
- `ground-control/internal/spiffe/provider.go` - add `//go:build !nospiffe`
- `ground-control/internal/spiffe/middleware.go` - add `//go:build !nospiffe`
- `ground-control/internal/spiffe/authorizer.go` - add `//go:build !nospiffe`
- `ground-control/internal/spiffe/source.go` - add `//go:build !nospiffe`

### Phase 9: Environment Variables

**Ground Control** (.env):
```bash
# Required
ADMIN_PASSWORD=<secure-password>

# User Auth (with defaults)
SESSION_DURATION=24h
LOCKOUT_DURATION=5m
MAX_LOGIN_ATTEMPTS=10

# Satellite Status
STALE_THRESHOLD=1h

# SPIFFE (optional)
SPIFFE_ENABLED=false
SPIFFE_TRUST_DOMAIN=harbor-satellite.local
SPIFFE_PROVIDER=sidecar
SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
```

**Satellite** (CLI flags or env):
```bash
# ZTR mode
--token=<bootstrap-token>
--ground-control-url=<url>

# SPIFFE mode
--spiffe-enabled=true
--spiffe-endpoint-socket=unix:///run/spire/sockets/agent.sock
--ground-control-url=<url>
```

## File Summary

### Copy from main (no changes)
- `ground-control/internal/auth/password.go`
- `ground-control/internal/auth/policy.go`
- `ground-control/sql/schema/009-013` (renumbered)
- `ground-control/sql/queries/users.sql`
- `ground-control/sql/queries/sessions.sql`
- `ground-control/sql/queries/login_attempts.sql`
- `ground-control/sql/queries/satellite_status.sql`

### Copy and adapt from main
- `ground-control/internal/server/middleware.go` (lockout config)
- `ground-control/internal/server/user_handlers.go`
- `ground-control/internal/server/auth_handlers.go`
- `ground-control/internal/server/bootstrap.go` (env var for password)
- `ground-control/internal/server/cleanup.go`

### Modify in plan-spiffe
- `ground-control/internal/server/server.go` (merge struct)
- `ground-control/internal/server/routes.go` (URL prefix structure)
- `ground-control/internal/server/satellite_handlers.go` (add status handlers)
- `internal/spiffe/client.go` (add build tag)
- `internal/crypto/aes_provider.go` (add build tag)
- `internal/identity/device_linux.go` (add build tag)
- `ground-control/internal/spiffe/*.go` (add build tags)

### Create new
- `internal/spiffe/client_stub.go`
- `internal/crypto/provider_stub.go`
- `internal/identity/device_stub.go`
- `ground-control/internal/spiffe/provider_stub.go`
- `ground-control/internal/spiffe/middleware_stub.go`

## Build Commands

```bash
# Satellite - Full build (default)
go build ./cmd/...

# Satellite - Minimal build (ZTR only)
go build -tags=nospiffe ./cmd/...

# Ground Control - Full build (default)
cd ground-control && go build ./...

# Ground Control - No SPIFFE
cd ground-control && go build -tags=nospiffe ./...
```

## Testing Checklist

1. [ ] User login/logout works
2. [ ] User CRUD works (admin only for create/delete)
3. [ ] Account lockout after 10 failures, unlocks after 5 min
4. [ ] Token-based ZTR works
5. [ ] SPIFFE-based ZTR works
6. [ ] Satellite sync with robot credentials works
7. [ ] Satellite sync with SPIFFE works
8. [ ] Active/stale satellite queries work
9. [ ] Rate limiting on auth endpoints works
10. [ ] `nospiffe` build compiles without SPIFFE dependencies
11. [ ] Human routes require user auth
12. [ ] Satellite routes reject human auth
