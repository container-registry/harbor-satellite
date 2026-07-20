# Migrate Ground Control Into the Harbor Satellite Module

## Summary

Migrate `ground-control` from a nested Go module into the root Harbor Satellite module. The repository should maintain a single `go.mod`, with `harbor-satellite` and `ground-control` built as separate binaries from one module.

## Background

The repository currently has a root Harbor Satellite Go module and a nested `ground-control/go.mod`. This creates two dependency graphs, two module boundaries, and extra friction for shared internal code, testing, dependency updates, and release workflows.

Ground Control is part of the Harbor Satellite project and does not need to be maintained as a separately consumable Go module. Moving it into the root module simplifies ownership and makes the repository easier to build, test, and maintain.

## Proposed Layout

```text
harbor-satellite/
├── cmd/
│   ├── satellite/
│   │   └── main.go
│   └── groundcontrol/
│       ├── cli/
│       │   └── root.go
│       └── server/
│           └── main.go
├── internal/
│   ├── satellite/
│   ├── groundcontrol/
│   ├── auth/
│   ├── database/
│   ├── harborhealth/
│   ├── middleware/
│   ├── models/
│   ├── server/
│   ├── spiffe/
│   ├── logger/
│   ├── crypto/
│   └── ...
├── pkg/
└── go.mod
```

## Design Guidelines

- Keep exactly one Go module at the repository root.
- Move executable entrypoints into `cmd/<binary-name>/main.go`.
- Keep `cmd` packages thin. They should only parse configuration, initialize dependencies, and call internal application code.
- Prefer `internal/satellite` and `internal/groundcontrol` for binary-specific application logic.
- Avoid a generic `internal/shared` package. Shared code should live in packages named after the behavior or domain they own, such as `internal/logger`, `internal/spiffe`, `internal/auth`, or `internal/database`.
- Keep code in `pkg` only when it is intentionally public and stable for external consumers. Code used only by repository binaries should live under `internal`.

## Migration Plan

### Phase 1: Move Satellite Command

- [ ] Move the satellite entrypoint from `cmd/main.go` to `cmd/satellite/main.go`.
- [ ] Update build, Docker, CI, and release references from `./cmd` to `./cmd/satellite`.
- [ ] Build `./cmd/satellite`.

### Phase 2: Refactor Satellite Internal Packages

- [ ] Move satellite-specific packages from `internal/*` into `internal/satellite/*`.
- [ ] Keep cross-cutting packages used by both binaries at the root of `internal`.
- [ ] Update satellite imports to use `github.com/container-registry/harbor-satellite/internal/satellite/...`.
- [ ] Run satellite package tests from the root module.

### Phase 3: Move Ground Control Runtime Packages

- [ ] Move `ground-control/internal/*` into the root `internal` tree.
- [ ] Place Ground Control orchestration code under `internal/groundcontrol`.
- [ ] Update Ground Control imports from `github.com/container-registry/harbor-satellite/ground-control/internal/...` to root-module `internal/...` imports.
- [ ] Resolve package name conflicts in the root `internal` tree.
- [ ] Run Ground Control package tests from the module that owns the moved packages.

### Phase 4: Move Ground Control Command

- [ ] Move `ground-control/main.go` to `cmd/groundcontrol/server/main.go`.
- [ ] Update the Ground Control command imports to root-module packages.
- [ ] Update build, Docker, CI, Helm, and release references from `ground-control` to `./cmd/groundcontrol/server`.
- [ ] Build `./cmd/groundcontrol/server`.

### Phase 5: Supporting Files and Packages

- [ ] Move repository-private code from `ground-control/pkg/*` into `internal`.
- [ ] Relocate SQL, migration, seed, and registry assets to their new root-module locations.
- [ ] Update embedded paths and runtime paths for moved assets.
- [ ] Update scripts and docs that reference `ground-control/`.

### Phase 6: Module Cleanup

- [ ] Remove `ground-control/go.mod`.
- [ ] Remove any dependency on `github.com/container-registry/harbor-satellite/ground-control`.
- [ ] Run `go mod tidy` from the repository root.
- [ ] Search for remaining `github.com/container-registry/harbor-satellite/ground-control` imports and remove them.

### Phase 7: Validation

- [ ] Build all three executables from the root module:

```sh
go build ./cmd/satellite
go build ./cmd/groundcontrol/server
go build ./cmd/groundcontrol/cli
```

- [ ] Run root module tests:

```sh
go test ./...
```

- [ ] Run linting and deployment checks if configured.
