---
title: "Satellite Configuration Reference"
weight: 1
description: "The satellite reads its settings from three places."
source: "docs/reference/satellite.md"
---

The satellite reads its settings from three places:

1. Command-line flags.
2. Environment variables, also loaded from a `.env` file in the working directory when one exists.
3. `config.json` in the config directory. Ground Control delivers `app_config` through the config state artifact, and the satellite writes the file itself after registration.

A flag wins over its environment variable. `--ground-control-url` (or `GROUND_CONTROL_URL`) also wins over `app_config.ground_control_url`, and a non-empty `USE_UNSECURE` overrides `app_config.use_unsecure`.

## Required Settings

| Setting | When |
|---|---|
| `--ground-control-url` / `GROUND_CONTROL_URL` | Always |
| `--harbor-registry-url` / `HARBOR_REGISTRY_URL` | Always, except with `--fallback-only` |
| `--token` / `TOKEN` | Unless `--spiffe-enabled` |
| `--registry-url` / `REGISTRY_URL` | With `--byo-registry` |

The satellite exits with a message naming the missing setting.

## Flags and Environment Variables

| Flag | Environment variable | Default | Description |
|---|---|---|---|
| `--token` | `TOKEN` | | One-time registration token from `POST /api/satellites`. |
| `--ground-control-url` | `GROUND_CONTROL_URL` | | Ground Control base URL. A path prefix and a trailing slash are allowed. An `http://` URL is refused unless `--use-unsecure` is set. |
| `--harbor-registry-url` | `HARBOR_REGISTRY_URL` | | Harbor as the satellite reaches it. Replaces the scheme and host of the registry and state URLs that Ground Control hands out. |
| `--use-unsecure` | `USE_UNSECURE` | `false` | Plain HTTP and no certificate verification toward Harbor and Ground Control. Development only. |
| `--json-logging` | | `true` | JSON log lines. `--json-logging=false` prints console logs. |
| `--config-dir` | `CONFIG_DIR` | `<user config dir>/satellite` | Directory for `config.json`, `state.json`, the journal and, by default, the OCI layout. `~/.config/satellite` on Linux, `~/Library/Application Support/satellite` on macOS. |
| `--registry-data-dir` | `REGISTRY_DATA_DIR` | `<config-dir>/oci` | Location of the local OCI image layout. |
| `--desired-state-file` | `DESIRED_STATE_FILE` | `<config-dir>/desired-state.json` | Bootstrap desired state, read only while `state.json` is empty. A corrupt file is logged and ignored. |
| `--shutdown-timeout` | `SHUTDOWN_TIMEOUT` | `30s` | How long a graceful shutdown waits for running replication before the process exits. An invalid value falls back to `30s`. |
| `--spiffe-enabled` | `SPIFFE_ENABLED` | `false` | Register and authenticate with an X.509 SVID instead of a token. |
| `--spiffe-endpoint-socket` | `SPIFFE_ENDPOINT_SOCKET` | `unix:///run/spire/sockets/agent.sock` | SPIFFE Workload API socket. |
| `--spiffe-expected-server-id` | `SPIFFE_EXPECTED_SERVER_ID` | | SPIFFE ID that Ground Control's certificate must carry. |
| `--byo-registry` | `BYO_REGISTRY` | `false` | Replicate into a registry you run at the site instead of the local OCI layout. |
| `--registry-url` | `REGISTRY_URL` | | URL of that registry. |
| `--registry-username` | `REGISTRY_USERNAME` | | Its username. |
| `--registry-password` | `REGISTRY_PASSWORD` | | Its password. Prefer the variable over the flag, which shows up in process listings. |
| `--proxy-mode` | `PROXY_MODE` | `proxy` | `proxy` fills a local miss from Harbor, `replica` serves only replicated content. Rejected together with `--fallback-only`. |
| `--proxy-port` | `PROXY_PORT` | `8585` | Port of the OCI proxy. It always listens on `127.0.0.1`. |
| `--proxy-write-timeout` | `PROXY_WRITE_TIMEOUT` | `60s` | Per-write deadline: a client that accepts no data for this long is cut off. A long download that makes progress is not. `0` disables it. |
| `--proxy-max-connections` | `PROXY_MAX_CONNECTIONS` | `0` | Concurrent proxy connections. `0` is unlimited. |
| `--proxy-streaming` | `PROXY_STREAMING` | `false` | In `proxy` mode, answer a manifest miss once the manifest is stored and stream blob misses while they are stored. Needs the local OCI layout; with a BYO registry it is ignored with a warning. |
| `--proxy-prefetch-concurrency` | `PROXY_PREFETCH_CONCURRENCY` | `2` | Background workers that complete and tag a graph after a streamed miss. Must be at least 1 with `--proxy-streaming`. |
| `--store-gc-grace` | `STORE_GC_GRACE` | `10m` | Unreferenced content in the OCI layout is deleted only after this long, and the collector runs at this interval. `0` deletes at once and runs no collector. |
| `--telemetry-addr` | `TELEMETRY_ADDR` | `127.0.0.1:8586` | Address of `/healthz` and `/metrics`. Empty disables them. A port in use fails startup. |
| `--mirrors` | | | Container runtime mirror configuration, `CRI:registry1,registry2`, repeatable. See [Container Runtime Integration](#container-runtime-integration). |
| `--no-registry-fallback` | `NO_REGISTRY_FALLBACK` | `false` | Write no runtime mirror configuration. |
| `--fallback-only` | | `false` | Apply the runtime mirror configuration and exit. No proxy runs, so the mirror endpoint is the BYO registry; without `--byo-registry` it warns and changes nothing. |
| `--direct-delivery` | `DIRECT_DELIVERY` | `false` | Experimental. Write image tarballs into the k3s or RKE2 agent image directory. |
| `--image-dir` | `IMAGE_DIR` | auto-detected | Directory for direct delivery. Startup fails when none is given or found. |

## Files in the Config Directory

| Path | Content |
|---|---|
| `config.json` | `state_config` (Harbor robot credentials, state URL, Ground Control session, credentials generation) and `app_config`. Contains secrets. |
| `prev_config.json` | The previous `config.json`, kept for change detection. |
| `state.json` | Replicated group state, written atomically (temporary file and rename). |
| `desired-state.json` | Optional bootstrap state, see `--desired-state-file`. |
| `journal/` | Edge journal segments: one record per replicated or deleted artifact, CRI result and pull-through fill. |
| `oci/` | OCI image layout of the local store, unless `--registry-data-dir` moves it. |

A layout whose `index.json` cannot be loaded is moved to `index.json.torn-<time>` at startup, and every group is replicated again.

## `app_config` Keys

Ground Control validates a config document against the same rules before it stores it and answers `422` when it does not pass.

| Key | Default | Description |
|---|---|---|
| `ground_control_url` | | Overridden by `--ground-control-url`. |
| `log_level` | `info` | `debug`, `info`, `warn`, `error`, `fatal` or `panic`. Applied without restart. |
| `use_unsecure` | `false` | See `--use-unsecure`. |
| `state_replication_interval` | `@every 00h00m30s` | How often the satellite checks its state artifacts. Applied without restart. |
| `register_satellite_interval` | `@every 00h00m05s` | Retry interval of registration. |
| `heartbeat_interval` | `@every 00h00m30s` | Heartbeat interval. Ground Control marks a satellite stale after three intervals without one. |
| `metrics.collect_cpu`, `collect_memory`, `collect_storage` | `false` | Include these readings in the heartbeat. |
| `bring_own_registry`, `local_registry.url`, `.username`, `.password` | | BYO registry, also set by the flags. |
| `tls.cert_file`, `key_file`, `ca_file`, `skip_verify` | | Client TLS toward Harbor, Ground Control and the BYO registry. |
| `spiffe.enabled`, `endpoint_socket`, `expected_server_id` | | Also set by the flags. |
| `encrypt_config` | `false` | Encrypt `config.json` with AES-256-GCM and a key derived from the device fingerprint. Real encryption only on Linux builds without the `nospiffe` tag. Read once at startup. |
| `registry_fallback.enabled`, `registries`, `runtimes` | | Runtime mirror configuration delivered by Ground Control. Takes priority over `--mirrors`. |
| `harbor_registry_url` | | Set from `--harbor-registry-url`. |
| `direct_delivery.enabled`, `image_dir` | | Set from the direct delivery flags. |
| `audit` | disabled | Audit log transports, see [Audit Logging](/docs/guides/audit-logging/). Applied without restart. |
| `replication_concurrency` | `0` | Groups replicating at once. `0` replicates every group at once. |
| `replication_bandwidth_bytes_per_sec` | `0` | Bandwidth cap of replication downloads from Harbor. `0` is unlimited. Proxy pull-through is not capped. |
| `journal.max_segment_bytes` | `4194304` | Size of one journal segment. |
| `journal.max_total_bytes` | `67108864` | Journal size cap. Records beyond it are dropped and counted. |
| `journal.upload_batch` | `500` | Records per heartbeat, capped at 1000. |
| `admission.mode` | `off` | Edge admission: `off`, `audit` (log denials, serve the request) or `enforce` (answer `DENIED`). |
| `admission.bundle` | | `{"model_version": "edge-admission/v1", "policies": [[sub, repo, ref, act, eft], ...]}`. See [Edge Admission](#edge-admission). |
| `peer_distribution` | absent | Copy artifacts from other satellites before Harbor. See [Peer Distribution](#peer-distribution). |
| `registry_auth.mode` | `robot` | `workload_identity` is refused until Harbor 3 offers its token exchange, see [Keyless Pulls](/docs/guides/keyless-pulls/). |

The satellite reloads `config.json` when the file changes. `log_level`, `state_replication_interval` and `audit` are applied on reload. `admission`, `peer_distribution` and `replication_bandwidth_bytes_per_sec` are read per request or per replication run, so they follow a reload too. The other keys (store, proxy, journal, TLS, encryption) apply after a restart.

## OCI Proxy

Unless `--fallback-only` is set, the satellite serves a pull-only OCI Distribution endpoint at `127.0.0.1:<proxy-port>`, backed by the local store (the OCI layout or the BYO registry).

- `proxy` mode: a local hit is served from the store. A `GET` miss is filled from Harbor and kept; a `HEAD` miss reads the metadata from Harbor without storing.
- `replica` mode: only replicated content is served. A miss is answered as unknown without contacting Harbor.
- Push, delete, tag list, referrers and catalog requests answer `UNSUPPORTED`.
- Requests from a non-loopback address are rejected. The listener is loopback only.

## Edge Admission

With `admission.mode` set to `audit` or `enforce`, every proxy request is checked against the bundle before it is served. A policy line is `[sub, repo, ref, act, eft]`:

| Field | Match |
|---|---|
| `sub` | Client identity or `*`. The loopback proxy has no client identity, so use `*`. |
| `repo` | Repository, Casbin `keyMatch` (`library/*`). |
| `ref` | Tag or digest, glob match, or `*`. A blob request matches every `allow` of its repository. |
| `act` | `pull_manifest`, `check_manifest`, `pull_blob`, `check_blob`, `check_registry` or `*`. |
| `eft` | `allow` or `deny`. A request needs one allow and no deny. |

In `enforce` mode a bundle that does not load also denies every request. Denials are logged with the matching rule.

## Peer Distribution

When `peer_distribution.enabled` is true, replication tries other satellites' registries before Harbor for each artifact. Eligible peers race, the first complete graph wins and cancels the others, and Harbor is used only when no peer completes. When Harbor's state list cannot be fetched, the satellite replicates its last known list from peers and skips the Harbor blob copy for that run.

| Key | Default | Description |
|---|---|---|
| `local_groups` | | Groups this satellite shares. |
| `static_peers`, `gc_peers` | | Peers as `{id, url, groups, username, password, tls}`. The URL is a registry endpoint reachable from this satellite, such as a peer's BYO registry. Ground Control does not fill `gc_peers` itself; both lists come from the config document. |
| `reachout_sats` | `group` | `group` uses peers that share a group with `local_groups`, `global` uses every peer. |
| `timeout` | `30s` | Per-attempt timeout, also the Harbor list fetch timeout while peers are enabled. |
| `retries` | `1` | Extra attempts after the first. |
| `concurrency` | `4` | Peers copying one artifact at once. |

An `https` peer is never contacted over plain HTTP, even with `use_unsecure`.

## Container Runtime Integration

The satellite can configure container runtimes to pull through it, with the upstream registry as fallback. In a normal run the mirror endpoint is the OCI proxy at `127.0.0.1:<proxy-port>`; with `--fallback-only` it is the BYO registry.

| Runtime | File | Notes |
|---|---|---|
| containerd | `/etc/containerd/certs.d/<registry>/hosts.toml` | Sets `config_path` in `config.toml`. |
| CRI-O, Podman | `/etc/containers/registries.conf` | |
| Docker | `/etc/docker/daemon.json` | docker.io only: `--mirrors=docker:true`. Needs a daemon restart. |

Priority: `app_config.registry_fallback` from Ground Control, then `--mirrors`, then `--no-registry-fallback`. Writing these files needs root. Each file is backed up before it is changed, and the result is reported in the next heartbeat.
