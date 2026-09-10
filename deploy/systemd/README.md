## Harbor Satellite systemd Service

Production-ready systemd service for running Harbor Satellite as a native Linux service with comprehensive security hardening, multiple authentication methods, and optional container runtime integration.

## Overview

Harbor Satellite is an edge registry fleet management application that enables distributed container image replication and local caching at edge locations. This systemd service implementation provides:

- **Boot-time startup** without Docker dependency
- **Native Linux integration** with journald logging, restart policies, and dependency management
- **Security hardening** using systemd sandboxing features
- **Multiple authentication methods** (token-based, SPIFFE join-token, SPIFFE x509pop, SPIFFE sshpop)
- **Optional CRI mirroring** for Docker, containerd, CRI-O, and Podman
- **Multi-instance support** for running multiple satellites on the same host

## Prerequisites

### Required

- Linux system with systemd (systemd version 240+)
- Harbor Satellite binary (build with `dagger call build --source=. --component=satellite export --path=./satellite`)
- Access to a Harbor Satellite Ground Control instance
- Authentication credentials (token or SPIFFE infrastructure)

### Optional

- SPIRE agent (for SPIFFE authentication methods)
- Container runtime (Docker, containerd, CRI-O, or Podman) if using CRI mirroring

## Quick Start

### 1. Build the binary

```bash
## Using Dagger
dagger call build --source=. --component=satellite export --path=./satellite

## Or using Go directly
go build -o satellite cmd/main.go
```

### 2. Install the service

```bash
sudo ./deploy/systemd/install-satellite.sh ./satellite
```

This creates:
- User and group: `harbor-satellite`
- Binary: `/opt/harbor-satellite/satellite`
- Config: `/etc/harbor-satellite/satellite.env`
- Data directory: `/var/lib/harbor-satellite/`
- Service: `/etc/systemd/system/harbor-satellite.service`
- Drop-in directory: `/etc/systemd/system/harbor-satellite.service.d/`

### 3. Configure authentication

Edit `/etc/harbor-satellite/satellite.env`:

```bash
sudo vim /etc/harbor-satellite/satellite.env
```

**For token-based authentication:**
```bash
GROUND_CONTROL_URL=https://ground-control.example.com:8080
TOKEN=your-satellite-token-here
```

**For SPIFFE authentication:**
```bash
GROUND_CONTROL_URL=https://ground-control.example.com:8080
SPIFFE_ENABLED=true
SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
SPIFFE_EXPECTED_SERVER_ID=spiffe://harbor-satellite.local/gc/main
```

### 4. Enable and start

```bash
sudo systemctl enable harbor-satellite.service
sudo systemctl start harbor-satellite.service
```

### 5. Verify

```bash
sudo systemctl status harbor-satellite.service
sudo journalctl -u harbor-satellite.service -f
```

Check endpoints:
```bash
curl http://localhost:8585/v2/  # Zot registry (should return {})
curl http://localhost:9090/health  # Metrics/health endpoint
```

## Configuration

### Environment Variables

The service loads configuration from `/etc/harbor-satellite/satellite.env`. See `examples/satellite.env.example` for all available options.

#### Required Variables

| Variable | Description |
|----------|-------------|
| `GROUND_CONTROL_URL` | URL of the Ground Control server (always required) |
| `TOKEN` | Authentication token (required unless SPIFFE enabled) |

#### SPIFFE Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SPIFFE_ENABLED` | `false` | Enable SPIFFE/SPIRE authentication |
| `SPIFFE_ENDPOINT_SOCKET` | `unix:///run/spire/sockets/agent.sock` | SPIRE agent socket path |
| `SPIFFE_EXPECTED_SERVER_ID` | (none) | Expected SPIFFE ID of Ground Control |

#### Optional Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `JSON_LOGGING` | `true` | Enable structured JSON logging |
| `USE_UNSECURE` | `false` | Disable TLS verification (dev only) |

### Runtime Configuration

Additional settings are managed in `/var/lib/harbor-satellite/config.json` after first run:
- State replication interval (default: 30s)
- Registration interval (default: 5s)
- Zot registry configuration (port, storage, logging)
- Log level
- TLS certificates

The satellite supports hot-reloading of `config.json` changes without restart.

### Drop-in Overrides

Systemd drop-in files in `/etc/systemd/system/harbor-satellite.service.d/` allow modular service customization:

| Drop-in | Purpose | When to Use |
|---------|---------|-------------|
| `10-spire-dependency.conf` | Add SPIRE agent dependency | SPIFFE authentication methods |
| `20-cri-mirroring.conf` | Relax security for CRI access | Using `--mirrors` flag |
| `30-mirrors-*.conf` | Configure CRI mirroring | Specific CRI configuration |

After adding drop-ins:
```bash
sudo systemctl daemon-reload
sudo systemctl restart harbor-satellite.service
```

## Authentication Methods

Harbor Satellite supports four authentication methods. Choose based on your infrastructure and security requirements.

### Method 1: Token-Based Authentication

**Use for:** Development, testing, simple deployments

**Security:** Basic bearer token, no mTLS

**Setup:**

1. Configure environment:
```bash
sudo cp examples/token-auth.env /etc/harbor-satellite/satellite.env
sudo vim /etc/harbor-satellite/satellite.env
```

2. Set values:
```bash
GROUND_CONTROL_URL=https://ground-control.example.com:8080
TOKEN=your-satellite-token-here
```

3. Start service:
```bash
sudo systemctl start harbor-satellite.service
```

### Method 2: SPIFFE Join Token

**Use for:** Zero-touch provisioning, initial fleet deployment

**Security:** mTLS via SPIFFE SVIDs with automatic rotation

**Prerequisites:**
- SPIRE agent installed and running
- Join token generated and configured

**Setup:**

1. Install SPIRE dependency drop-in:
```bash
sudo cp examples/10-spire-dependency.conf /etc/systemd/system/harbor-satellite.service.d/
sudo systemctl daemon-reload
```

2. Configure environment:
```bash
sudo cp examples/spiffe-jointoken.env /etc/harbor-satellite/satellite.env
sudo vim /etc/harbor-satellite/satellite.env
```

3. Set values:
```bash
GROUND_CONTROL_URL=https://ground-control.example.com:8080
SPIFFE_ENABLED=true
SPIFFE_ENDPOINT_SOCKET=unix:///run/spire/sockets/agent.sock
SPIFFE_EXPECTED_SERVER_ID=spiffe://harbor-satellite.local/gc/main
```

4. Verify SPIRE socket access:
```bash
sudo -u harbor-satellite test -S /run/spire/sockets/agent.sock || \
  sudo usermod -a -G spire harbor-satellite
```

5. Start service:
```bash
sudo systemctl start harbor-satellite.service
```

6. Verify SPIFFE connection:
```bash
sudo journalctl -u harbor-satellite.service | grep "SPIFFE"
```

### Method 3: SPIFFE X.509 Proof-of-Possession

**Use for:** Pre-provisioned hardware, manufacturing environments

**Security:** Hardware-bound identity, mTLS

**Prerequisites:**
- SPIRE agent with x509pop attestation
- X.509 certificates pre-provisioned on device

**Setup:**

Follow Method 2 setup steps, using `examples/spiffe-x509pop.env` as the template.

### Method 4: SPIFFE SSH Proof-of-Possession

**Use for:** Environments with existing SSH PKI

**Security:** SSH certificate-based identity, mTLS

**Prerequisites:**
- SPIRE agent with sshpop attestation
- SSH CA infrastructure configured

**Setup:**

Follow Method 2 setup steps, using `examples/spiffe-sshpop.env` as the template.

## Container Runtime Mirroring

Harbor Satellite can configure container runtimes to use the local Zot registry as a mirror, with fallback to upstream registries.

**WARNING:** CRI mirroring requires elevated privileges. Only enable if needed.

### Enabling CRI Mirroring

1. Install security override drop-in:
```bash
sudo cp examples/20-cri-mirroring.conf /etc/systemd/system/harbor-satellite.service.d/
```

This grants:
- `CAP_SYS_ADMIN` and `CAP_DAC_OVERRIDE` capabilities
- Write access to `/etc/docker`, `/etc/containerd`, `/etc/containers`, `/etc/crio`
- Access to `systemctl` for restarting container runtimes

2. Install CRI-specific configuration:

**For containerd:**
```bash
sudo cp examples/30-mirrors-containerd.conf /etc/systemd/system/harbor-satellite.service.d/
```

**For Docker:**
```bash
sudo cp examples/30-mirrors-docker.conf /etc/systemd/system/harbor-satellite.service.d/
```

**For Podman:**
```bash
sudo cp examples/30-mirrors-podman.conf /etc/systemd/system/harbor-satellite.service.d/
```

3. Reload and restart:
```bash
sudo systemctl daemon-reload
sudo systemctl restart harbor-satellite.service
```

4. Verify CRI configuration:

**For Docker:**
```bash
sudo cat /etc/docker/daemon.json | jq .
sudo systemctl status docker.service
```

**For containerd:**
```bash
sudo cat /etc/containerd/config.toml | grep -A 10 mirrors
sudo systemctl status containerd.service
```

### Custom Mirror Configuration

Edit the mirrors drop-in to customize registries:

```bash
sudo vim /etc/systemd/system/harbor-satellite.service.d/30-mirrors-containerd.conf
```

Example for multiple CRIs:
```ini
[Service]
ExecStart=
ExecStart=/opt/harbor-satellite/satellite --mirrors=containerd:docker.io,quay.io,gcr.io --mirrors=podman:docker.io
```

### Security Implications

CRI mirroring relaxes security restrictions:
- Security score drops from ~9.0/10 to ~6.5/10
- Service can modify CRI configuration files
- Service can restart Docker/containerd/CRI-O/Podman
- Requires `CAP_SYS_ADMIN` capability

**Recommendation:** Only enable CRI mirroring on edge nodes that require local registry caching.

## Service Management

### Basic Commands

```bash
## Start service
sudo systemctl start harbor-satellite.service

## Stop service
sudo systemctl stop harbor-satellite.service

## Restart service
sudo systemctl restart harbor-satellite.service

## Enable on boot
sudo systemctl enable harbor-satellite.service

## Disable on boot
sudo systemctl disable harbor-satellite.service

## Check status
sudo systemctl status harbor-satellite.service

## Check if running
sudo systemctl is-active harbor-satellite.service

## Check if enabled
sudo systemctl is-enabled harbor-satellite.service
```

### Viewing Logs

```bash
## Follow logs (tail -f)
sudo journalctl -u harbor-satellite.service -f

## Last 100 lines
sudo journalctl -u harbor-satellite.service -n 100

## Logs since 1 hour ago
sudo journalctl -u harbor-satellite.service --since "1 hour ago"

## Logs for specific time range
sudo journalctl -u harbor-satellite.service --since "2024-01-15 10:00" --until "2024-01-15 11:00"

## Errors only
sudo journalctl -u harbor-satellite.service -p err

## JSON output
sudo journalctl -u harbor-satellite.service -o json-pretty

## Current boot logs
sudo journalctl -u harbor-satellite.service -b
```

### Hot-Reload Configuration

Many configuration changes can be applied without restart:

```bash
## Edit runtime config
sudo vim /var/lib/harbor-satellite/config.json

## File watcher will detect changes automatically
sudo journalctl -u harbor-satellite.service | grep "Configuration reload"
```

For environment variable changes, restart is required:
```bash
sudo vim /etc/harbor-satellite/satellite.env
sudo systemctl restart harbor-satellite.service
```

## Monitoring

### Health Endpoints

Harbor Satellite exposes two endpoints for monitoring:

```bash
## Zot registry health (OCI distribution API)
curl http://localhost:8585/v2/

## Application health and metrics
curl http://localhost:9090/health
curl http://localhost:9090/metrics
```

### Service Status

```bash
## Detailed status
sudo systemctl status harbor-satellite.service

## Check dependencies
sudo systemctl list-dependencies harbor-satellite.service

## Resource usage
sudo systemctl show harbor-satellite.service -p MemoryCurrent,CPUUsageNSec

## Live resource monitoring
sudo systemd-cgtop
```

### Security Analysis

```bash
## Analyze service security hardening
sudo systemd-analyze security harbor-satellite.service

## Verify unit file syntax
sudo systemd-analyze verify /etc/systemd/system/harbor-satellite.service
```

Expected security score:
- Base service (no CRI mirroring): ~9.0/10
- With CRI mirroring: ~6.5/10

## Troubleshooting

### Service Fails to Start

1. Check service status:
```bash
sudo systemctl status harbor-satellite.service
```

2. View full logs:
```bash
sudo journalctl -u harbor-satellite.service -n 100 --no-pager
```

3. Verify configuration:
```bash
sudo cat /etc/harbor-satellite/satellite.env
```

4. Test binary manually:
```bash
sudo -u harbor-satellite /opt/harbor-satellite/satellite --help
```

### Common Error Codes

| Exit Code | Meaning | Solution |
|-----------|---------|----------|
| 217/USER | User doesn't exist | Re-run installation script |
| 226/NAMESPACE | Security restriction too strict | Check drop-in configuration |
| 203/EXEC | Binary not found or not executable | Verify `/opt/harbor-satellite/satellite` exists |
| 200/CHDIR | WorkingDirectory doesn't exist | Create `/var/lib/harbor-satellite/` |

### SPIFFE Connection Issues

1. Verify SPIRE agent is running:
```bash
sudo systemctl status spire-agent.service
```

2. Check socket permissions:
```bash
sudo ls -la /run/spire/sockets/agent.sock
sudo -u harbor-satellite test -S /run/spire/sockets/agent.sock
```

3. Add user to SPIRE group if needed:
```bash
sudo usermod -a -G spire harbor-satellite
sudo systemctl restart harbor-satellite.service
```

4. Check SPIFFE logs:
```bash
sudo journalctl -u harbor-satellite.service | grep -i spiffe
```

### CRI Mirroring Issues

1. Verify drop-in is installed:
```bash
sudo ls -la /etc/systemd/system/harbor-satellite.service.d/
```

2. Check CRI configuration was updated:
```bash
## Docker
sudo cat /etc/docker/daemon.json

## containerd
sudo cat /etc/containerd/config.toml | grep -A 10 mirrors
```

3. Verify CRI service restarted:
```bash
sudo journalctl -u docker.service -n 20
```

4. Check satellite logs for CRI errors:
```bash
sudo journalctl -u harbor-satellite.service | grep -i "mirror\|docker\|containerd"
```

### Permission Denied Errors

1. Verify file ownership:
```bash
sudo ls -la /var/lib/harbor-satellite/
sudo ls -la /etc/harbor-satellite/
```

2. Check SELinux/AppArmor (if enabled):
```bash
## SELinux
sudo ausearch -m avc -ts recent | grep harbor-satellite

## AppArmor
sudo dmesg | grep -i apparmor | grep harbor-satellite
```

3. Verify systemd security directives:
```bash
sudo systemd-analyze security harbor-satellite.service
```

### Restart Loop

1. Check restart limits:
```bash
sudo systemctl status harbor-satellite.service | grep -i "start limit"
```

2. Reset failed state:
```bash
sudo systemctl reset-failed harbor-satellite.service
```

3. Increase rate limiting (if legitimate):
```bash
sudo systemctl edit harbor-satellite.service
```

Add:
```ini
[Service]
StartLimitBurst=10
StartLimitIntervalSec=600
```

## Multi-Instance Deployment

Run multiple satellite instances on the same host using the template service.

### Setup

1. Create instance-specific directories:
```bash
sudo mkdir -p /var/lib/harbor-satellite-edge01
sudo mkdir -p /var/lib/harbor-satellite-edge02
sudo chown harbor-satellite:harbor-satellite /var/lib/harbor-satellite-*
```

2. Create instance-specific configuration:
```bash
sudo cp /etc/harbor-satellite/satellite.env /etc/harbor-satellite/satellite-edge01.env
sudo cp /etc/harbor-satellite/satellite.env /etc/harbor-satellite/satellite-edge02.env
```

3. Edit each configuration with different tokens:
```bash
sudo vim /etc/harbor-satellite/satellite-edge01.env  # Set TOKEN=token-edge01
sudo vim /etc/harbor-satellite/satellite-edge02.env  # Set TOKEN=token-edge02
```

4. Start instances:
```bash
sudo systemctl enable harbor-satellite@edge01.service
sudo systemctl enable harbor-satellite@edge02.service
sudo systemctl start harbor-satellite@edge01.service
sudo systemctl start harbor-satellite@edge02.service
```

5. Verify:
```bash
sudo systemctl status harbor-satellite@edge01.service
sudo systemctl status harbor-satellite@edge02.service
```

### Zot Port Conflicts

Multiple instances cannot share the same Zot registry port (default: 8585).

After first run, edit each instance's `config.json`:
```bash
sudo vim /var/lib/harbor-satellite-edge01/config.json
```

Change `zot_config.http.port`:
```json
{
  "zot_config": {
    "http": {
      "port": "8586"
    }
  }
}
```

**Note:** Hot-reload will detect the change automatically.

### Managing Instances

```bash
## List all instances
sudo systemctl list-units 'harbor-satellite@*'

## Stop all instances
sudo systemctl stop 'harbor-satellite@*'

## Restart specific instance
sudo systemctl restart harbor-satellite@edge01.service

## View logs for specific instance
sudo journalctl -u harbor-satellite@edge01.service -f
```

## Upgrading

### Binary Upgrade

1. Stop service:
```bash
sudo systemctl stop harbor-satellite.service
```

2. Backup current binary:
```bash
sudo cp /opt/harbor-satellite/satellite /opt/harbor-satellite/satellite.backup
```

3. Install new binary:
```bash
sudo install -m 755 ./satellite-new /opt/harbor-satellite/satellite
```

4. Start service:
```bash
sudo systemctl start harbor-satellite.service
```

5. Verify:
```bash
sudo systemctl status harbor-satellite.service
sudo journalctl -u harbor-satellite.service -n 50
```

### Service File Upgrade

1. Stop service:
```bash
sudo systemctl stop harbor-satellite.service
```

2. Update service file:
```bash
sudo cp deploy/systemd/harbor-satellite.service /etc/systemd/system/
```

3. Reload systemd:
```bash
sudo systemctl daemon-reload
```

4. Start service:
```bash
sudo systemctl start harbor-satellite.service
```

### Configuration Upgrade

For environment variables:
```bash
sudo vim /etc/harbor-satellite/satellite.env
sudo systemctl restart harbor-satellite.service
```

For runtime config (hot-reload):
```bash
sudo vim /var/lib/harbor-satellite/config.json
## Automatic reload, no restart needed
```

## Security

### Hardening Features

The base service implements strict systemd sandboxing:

- **Filesystem isolation:** Read-only system, private `/tmp`, no home directory access
- **User namespace:** Isolated user namespace
- **System call filtering:** Restricted to safe syscalls
- **Capabilities:** No Linux capabilities required
- **Memory protection:** W^X enforcement (MemoryDenyWriteExecute)
- **Network isolation:** Limited to IPv4, IPv6, and Unix sockets
- **Device isolation:** No device access

### Security Trade-offs

CRI mirroring requires relaxed security:

| Feature | Base Service | With CRI Mirroring |
|---------|--------------|-------------------|
| Capabilities | None | CAP_SYS_ADMIN, CAP_DAC_OVERRIDE |
| User namespace | Isolated | Shared (PrivateUsers=no) |
| Filesystem access | `/var/lib/harbor-satellite` only | + `/etc/docker`, `/etc/containerd`, etc. |
| System calls | Restricted | + privileged syscalls |
| Security score | ~9.0/10 | ~6.5/10 |

### Best Practices

1. **Use SPIFFE authentication** for production deployments
2. **Enable CRI mirroring only when needed** for local caching
3. **Run security analysis regularly:**
   ```bash
   sudo systemd-analyze security harbor-satellite.service
   ```
4. **Monitor logs for suspicious activity:**
   ```bash
   sudo journalctl -u harbor-satellite.service -p warning
   ```
5. **Keep binary updated** to latest security patches
6. **Use TLS** (avoid `USE_UNSECURE=true` in production)
7. **Restrict network access** to Ground Control URL only (firewall rules)

## References

- [Harbor Satellite Documentation](https://github.com/goharbor/harbor-satellite)
- [systemd Service Management](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [systemd Security Directives](https://www.freedesktop.org/software/systemd/man/systemd.exec.html)
- [SPIRE Documentation](https://spiffe.io/docs/latest/deploying/)
- [SPIRE Agent Installation](https://spiffe.io/docs/latest/deploying/install-agents/)
- [OCI Distribution Spec](https://github.com/opencontainers/distribution-spec)

## Support

For issues and questions:
- GitHub Issues: https://github.com/goharbor/harbor-satellite/issues
- Discussions: https://github.com/goharbor/harbor-satellite/discussions
