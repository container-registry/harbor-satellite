# Running Harbor Satellite with systemd

These files run the satellite as a native systemd service on an edge host, without Docker.

| File | Purpose |
|------|---------|
| `harbor-satellite.service` | Main unit |
| `harbor-satellite@.service` | Template for running several satellites on one host |
| `install-satellite.sh` | Installs the binary, units and an environment file |
| `examples/satellite.env.example` | Environment file for token authentication |
| `examples/spiffe.env.example` | Environment file for SPIFFE authentication |
| `examples/10-spire-dependency.conf` | Drop-in that orders the satellite after `spire-agent.service` |
| `examples/20-cri-mirroring.conf` | Drop-in that lets the satellite edit container runtime configs |
| `examples/30-mirrors-*.conf` | Drop-ins that add a `--mirrors` flag for containerd, Docker or Podman |

## Install

Build a static binary (the unit sets `MemoryDenyWriteExecute=`, so keep `CGO_ENABLED=0`):

```bash
CGO_ENABLED=0 go build -o satellite ./cmd/satellite
sudo ./examples/deploy/systemd/install-satellite.sh ./satellite
```

The script creates the `harbor-satellite` system user and installs:

- `/opt/harbor-satellite/satellite`
- `/etc/systemd/system/harbor-satellite.service` and `harbor-satellite@.service`
- `/etc/harbor-satellite/satellite.env` (only if it does not exist yet)
- an empty drop-in directory at `/etc/systemd/system/harbor-satellite.service.d/`

Edit `/etc/harbor-satellite/satellite.env`. `GROUND_CONTROL_URL` and `HARBOR_REGISTRY_URL` are always required. Add `TOKEN` for token registration, or the `SPIFFE_*` variables from `examples/spiffe.env.example`. Then start the service:

```bash
sudo systemctl enable --now harbor-satellite.service
systemctl status harbor-satellite.service
journalctl -u harbor-satellite.service -f
```

## Where things live

The unit uses `StateDirectory=harbor-satellite` and sets `CONFIG_DIR=/var/lib/harbor-satellite`. systemd creates that directory and gives it to the service user. It holds `config.json`, `state.json` and the OCI image layout in `oci/`.

`config.json` is written after registration and is watched for changes, so log level and replication interval edits apply without a restart. Other changes need `systemctl restart harbor-satellite`.

To keep images on another disk, set `REGISTRY_DATA_DIR` and allow the path in a drop-in:

```ini
# /etc/systemd/system/harbor-satellite.service.d/40-data-dir.conf
[Service]
ReadWritePaths=/srv/harbor-satellite/oci
```

The directory must be writable by the `harbor-satellite` user.

## Local proxy

The satellite serves a pull-only OCI registry on `127.0.0.1:8585`. Change the port with `PROXY_PORT`. It always binds to loopback, so nothing on the network can reach it. In the default `proxy` mode a missing image is fetched from Harbor and stored locally. With `PROXY_MODE=replica` only replicated content is served.

```bash
curl -s http://127.0.0.1:8585/v2/
```

## SPIFFE authentication

Install and configure the SPIRE agent first. The attestation method (join token, x509pop or sshpop) is set up in the agent, see `examples/deploy/spiffe/`. On the satellite side all three use the same variables:

```bash
sudo install -m 0600 examples/deploy/systemd/examples/spiffe.env.example /etc/harbor-satellite/satellite.env
sudo cp examples/deploy/systemd/examples/10-spire-dependency.conf /etc/systemd/system/harbor-satellite.service.d/
sudo systemctl daemon-reload
sudo systemctl restart harbor-satellite
```

The drop-in assumes the agent runs as `spire-agent.service` and listens on `/run/spire/sockets/agent.sock`. Adjust both if your setup differs.

## Container runtime mirroring

The satellite can configure containerd, Docker, Podman or CRI-O to pull through its local proxy, with the upstream registry as fallback. This happens when you pass `--mirrors`, or when Ground Control enables `registry_fallback` in the satellite's config.

The runtime configs are root-owned files under `/etc`, and Docker has to be restarted after `daemon.json` changes. So `20-cri-mirroring.conf` runs the service as root, gives it write access to `/etc/docker`, `/etc/containerd` and `/etc/containers`, and leaves the rest of the sandbox as it is. Only install it if you need mirroring.

```bash
D=/etc/systemd/system/harbor-satellite.service.d
sudo cp examples/deploy/systemd/examples/20-cri-mirroring.conf "$D/"
sudo cp examples/deploy/systemd/examples/30-mirrors-containerd.conf "$D/"
sudo systemctl daemon-reload
sudo systemctl restart harbor-satellite
```

`--mirrors` has no environment variable, so the `30-mirrors-*.conf` files override `ExecStart`. Use only one of them. To mirror for more than one runtime, repeat the flag on a single `ExecStart` line:

```ini
[Service]
ExecStart=
ExecStart=/opt/harbor-satellite/satellite --mirrors=containerd:docker.io,quay.io --mirrors=podman:docker.io
```

Docker can only mirror docker.io, so its flag is `--mirrors=docker:true`.

## Several satellites on one host

`harbor-satellite@.service` reads `/etc/harbor-satellite/satellite-<name>.env` and keeps its state in `/var/lib/harbor-satellite-<name>`. Each instance needs its own token and its own `PROXY_PORT`, since they all bind to `127.0.0.1`.

```bash
sudo install -m 0600 examples/deploy/systemd/examples/satellite.env.example /etc/harbor-satellite/satellite-edge1.env
# set TOKEN and PROXY_PORT=8586 in that file
sudo systemctl enable --now harbor-satellite@edge1.service
```

Leave container runtime mirroring to a single instance. All instances would write the same runtime config files.

## Sandboxing

The unit runs as an unprivileged user with no capabilities, a read-only view of the filesystem except its state directory, private `/tmp` and devices, and a seccomp filter. `systemd-analyze security harbor-satellite.service` reports an exposure of about 1.1.

A few choices are deliberate:

- `ProcSubset=pid` is not set. The status report reads `/proc/stat` and `/proc/meminfo`, and the device fingerprint reads `/proc/cpuinfo`.
- `AF_NETLINK` is allowed because the device fingerprint lists network interfaces.
- `TimeoutStopSec=45s` is longer than the default `SHUTDOWN_TIMEOUT` of 30s, so the satellite can finish in-flight work and save state before systemd kills it.

If the service fails with permission or `status=31/SYS` errors after you change a setting, check `journalctl -u harbor-satellite -b` and relax one directive at a time in a drop-in.

## Upgrading

```bash
sudo install -m 0755 ./satellite /opt/harbor-satellite/satellite
sudo systemctl restart harbor-satellite
```

Running `install-satellite.sh` again also updates the unit files and keeps the existing environment file.
