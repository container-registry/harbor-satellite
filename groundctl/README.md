# groundctl

`groundctl` is the command-line interface for managing Harbor Satellite fleets via the Ground Control API.

## Installation

```bash
# Build from source
task build-groundctl

# The binary is placed at bin/groundctl (or bin/groundctl.exe on Windows)
```

## Configuration

`groundctl` reads configuration from flags or environment variables:

| Flag | Environment Variable | Description |
|---|---|---|
| `--gc-url` | `GROUNDCTL_URL` | Ground Control base URL (e.g. `http://ground-control:8080`) |
| `--token` | `GROUNDCTL_TOKEN` | Bearer token obtained from `groundctl login` |
| `--output` / `-o` | — | Output format: `table` (default) or `json` |

Environment variables are used as fallbacks when flags are not set.

## Commands

### `groundctl satellite`

Manage satellites registered with Ground Control.

```
groundctl satellite list [flags]
groundctl satellite get <name>
groundctl satellite register <name> --config <config> [--groups <g1,g2>]
groundctl satellite delete <name> [--force]
groundctl satellite status <name>
groundctl satellite images <name>
```

#### `satellite list`

List all registered satellites.

```bash
# List all satellites
groundctl satellite list

# Filter by name prefix
groundctl satellite list --name edge-tokyo

# Paginate results
groundctl satellite list --limit 20 --offset 0

# Show only active satellites (checked in within the last 2 minutes)
groundctl satellite list --active

# Show only stale satellites
groundctl satellite list --stale

# JSON output
groundctl satellite list --output json
```

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--name` | — | Filter satellites by name prefix |
| `--limit` | 0 (all) | Maximum number of results to return |
| `--offset` | 0 | Number of results to skip |
| `--sort` | `name` | Sort field: `name`, `created_at`, `last_seen` |
| `--order` | `asc` | Sort order: `asc` or `desc` |
| `--active` | false | Show only active satellites |
| `--stale` | false | Show only stale satellites |

**Example output:**

```
NAME               STATUS    LAST SEEN    HEARTBEAT    CREATED
edge-tokyo-01      Active    45s ago      30s          3d ago
edge-osaka-02      Idle      8m ago       30s          3d ago
edge-seoul-03      Stale     2h ago       30s          7d ago
```

#### `satellite get`

```bash
groundctl satellite get edge-tokyo-01
```

#### `satellite register`

Register a new satellite and print its one-time token.

```bash
groundctl satellite register edge-tokyo-01 \
  --config prod-config \
  --groups base-images,ml-models
```

#### `satellite delete`

```bash
# With confirmation prompt
groundctl satellite delete edge-tokyo-01

# Skip confirmation
groundctl satellite delete edge-tokyo-01 --force
```

#### `satellite status`

Show the latest sync status reported by a satellite.

```bash
groundctl satellite status edge-tokyo-01
```

**Example output:**

```
Satellite:     edge-tokyo-01
Activity:      syncing
State Digest:  sha256:abc123…
Config Digest: sha256:def456…
CPU:           12%
Memory:        256.0 MiB
Storage:       4.2 GiB
Images Cached: 14
Last Sync:     342ms
Reported At:   45s ago
```

#### `satellite images`

List images currently cached on a satellite.

```bash
groundctl satellite images edge-tokyo-01
```

---

### `groundctl group`

Manage image groups in Ground Control.

```bash
groundctl group list
groundctl group get <name>
groundctl group add-satellite <satellite> --group <name>
groundctl group remove-satellite <satellite> --group <name>
```

---

### `groundctl config`

Manage satellite configurations.

```bash
groundctl config list
groundctl config get <name>
groundctl config delete <name>
```

---

### `groundctl apply`

Apply a declarative `SatelliteFleet` manifest to Ground Control. This is the GitOps entrypoint for managing fleets at scale.

```bash
# Apply a fleet manifest
groundctl apply -f fleet.yaml

# Preview changes without applying (dry run)
groundctl apply -f fleet.yaml --dry-run
```

**Flags:**

| Flag | Description |
|---|---|
| `-f` / `--file` | Path to the fleet manifest YAML file (required) |
| `--dry-run` | Preview changes without applying them |

#### Fleet Manifest Schema

```yaml
apiVersion: satellite.harbor.io/v1alpha1
kind: SatelliteFleet
metadata:
  name: prod-asia

spec:
  # Default config applied to all satellites unless overridden per entry
  configName: prod-config

  # Default image groups for all satellites unless overridden per entry
  groups:
    - base-images

  satellites:
    - name: edge-tokyo-01
      groups:
        - base-images
        - ml-models          # Additional groups for this satellite

    - name: edge-osaka-02    # Inherits fleet-level config and groups

    - name: edge-seoul-03
      configName: staging-config   # Override fleet-level config
      groups:
        - base-images
```

#### Reconcile Behaviour

`groundctl apply` performs a three-way diff:

| State | Action |
|---|---|
| In manifest, not in Ground Control | `POST /api/satellites` — satellite is registered |
| In Ground Control, not in manifest | `DELETE /api/satellites/{name}` — satellite is deleted |
| In both | Satellite is left unchanged |

**Example output:**

```
  + satellite/edge-tokyo-01               created
  + satellite/edge-osaka-02               created
  - satellite/edge-berlin-05              deleted
    satellite/edge-seoul-03               unchanged

Applied: 2 created, 1 deleted, 0 updated, 1 unchanged
```

With `--dry-run`:

```
Dry run — no changes will be applied.

  + satellite/edge-tokyo-01               created
  + satellite/edge-osaka-02               created
  - satellite/edge-berlin-05              deleted
    satellite/edge-seoul-03               unchanged

Applied: 2 created, 1 deleted, 0 updated, 1 unchanged
```

---

### `groundctl version`

```bash
groundctl version
# groundctl v0.1.0
```

The version is injected at build time:

```bash
go build -ldflags "-X github.com/container-registry/harbor-satellite/groundctl/cmd.Version=v0.1.0" .
```

## Examples

### Register a fleet from a YAML file

```bash
export GROUNDCTL_URL=http://ground-control:8080
export GROUNDCTL_TOKEN=<token>

groundctl apply -f examples/fleet.yaml
```

### Check which satellites are stale

```bash
groundctl satellite list --stale --output json | jq '.[].name'
```

### Remove a satellite from a group

```bash
groundctl group remove-satellite edge-tokyo-01 --group ml-models
```

### Watch satellite status

```bash
watch -n 10 groundctl satellite status edge-tokyo-01
```
