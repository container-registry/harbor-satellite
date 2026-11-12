# Installation

Harbor Satellite has two components: Ground Control runs next to Harbor, and a satellite runs at each site. Install Ground Control first.

## Requirements

| Component | Needs |
|---|---|
| Harbor | Harbor 2.x and an account that can create projects and robot accounts. Ground Control creates the `satellite` project for state artifacts. |
| Ground Control | PostgreSQL (see [supported versions](operations/database.md#supported-postgresql-versions)), network access to Harbor, and HTTPS in front of it for production. |
| Satellite | Linux (amd64, arm64, ppc64le, s390x, riscv64 and more) or macOS, network access to Harbor and Ground Control during sync, and disk for the replicated artifacts. Root only to write container runtime configuration. |
| SPIRE (optional) | A SPIRE server and agents for SVID-based registration and mTLS. |

## Release Artifacts

Each release on the [releases page](https://github.com/container-registry/harbor-satellite/releases) provides:

| Artifact | Name |
|---|---|
| Container images (linux amd64, arm64, ppc64le, s390x, riscv64), signed with Cosign | `registry.goharbor.io/harbor-satellite/satellite`, `registry.goharbor.io/harbor-satellite/ground-control` |
| Archives with SBOM | `harbor-satellite_<version>_<os>_<arch>.tar.gz`, `groundcontrol_<version>_<os>_<arch>.tar.gz` |
| Packages (deb, rpm, apk, archlinux) | `harbor-satellite` (binary `satellite`), `groundcontrol` |
| Checksums | `checksums.txt` |

Verify an image signature:

```bash
cosign verify registry.goharbor.io/harbor-satellite/satellite:<version> \
  --certificate-identity-regexp 'https://github.com/container-registry/harbor-satellite/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The packages install the binary only. Create the service unit yourself, see [below](#satellite-as-a-systemd-service).

## Ground Control

### Docker Compose

The repository's `docker-compose.yml` starts PostgreSQL and Ground Control built from source. It is meant for evaluation. See the [token quickstart](../examples/deploy/no-spiffe/quickstart.md).

### Kubernetes with Helm

The chart is in [`examples/deploy/helm/groundcontrol`](../examples/deploy/helm/groundcontrol). It can run an internal PostgreSQL or use an external one.

```bash
helm install groundcontrol ./examples/deploy/helm/groundcontrol \
  --namespace harbor-satellite --create-namespace \
  --set harbor.url=https://harbor.example.com \
  --set harbor.password='<harbor password>' \
  --set adminPassword='<initial admin password>' \
  --set database.internal.enabled=false \
  --set database.host=postgres.example.com \
  --set database.password='<db password>' \
  --set database.sslMode=verify-full
```

`database.sslMode` empty means `disable` for the internal PostgreSQL and `require` for an external one. The chart has values for Harbor, the database, SPIFFE, the robot lifetime and the service only, and no `extraEnv`. Other variables, such as `RATE_LIMIT_SPLIT` or `ROBOT_ROTATION_ENABLED`, need a change to the chart's ConfigMap template or a patch of the deployment.

### Container

```bash
docker run -d --name ground-control -p 8080:8080 \
  -e HARBOR_URL=https://harbor.example.com -e HARBOR_USERNAME=admin -e HARBOR_PASSWORD='<password>' \
  -e DB_HOST=db.example.com -e DB_PORT=5432 -e DB_DATABASE=groundcontrol \
  -e DB_USERNAME=groundcontrol -e DB_PASSWORD='<password>' -e DB_SSLMODE=verify-full \
  -e ADMIN_PASSWORD='<initial admin password>' \
  registry.goharbor.io/harbor-satellite/ground-control:<version> serve
```

The image's entrypoint is `groundcontrol`; without `serve` it prints the client help.

### Binary

```bash
groundcontrol serve                # API and embedded worker
groundcontrol serve --worker=off   # API only
groundcontrol worker               # worker only
```

Migrations are embedded, so no source checkout is needed.

### Production Settings

At minimum:

- HTTPS: `TLS_CERT_FILE`/`TLS_KEY_FILE`, or a TLS-terminating proxy, see [TLS](operations/tls.md).
- `DB_SSLMODE=verify-full` and `DB_SSLROOTCERT` for a remote database.
- `RATE_LIMIT_SPLIT=true`.
- `ROBOT_ROTATION_ENABLED=true` with `DATA_ENCRYPTION_KEY`.
- A readiness probe on `GET /health` and a liveness probe on `GET /ping`.

All variables are in the [Ground Control Configuration Reference](reference/ground-control.md).

## Satellite

### Container

```bash
docker run -d --name satellite --network host \
  -v /var/lib/satellite:/data \
  -e CONFIG_DIR=/data \
  -e GROUND_CONTROL_URL=https://gc.example.com \
  -e HARBOR_REGISTRY_URL=https://harbor.example.com \
  -e TOKEN='<token>' \
  registry.goharbor.io/harbor-satellite/satellite:<version>
```

The OCI proxy listens on the container's `127.0.0.1:8585`. Host networking makes it the host's loopback, so the node's container runtime can pull from it. The image runs as a non-root user; the data directory must be writable for it.

### Satellite as a systemd Service

```ini
[Unit]
Description=Harbor Satellite
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/harbor-satellite/satellite.env
ExecStart=/usr/bin/satellite --config-dir /var/lib/harbor-satellite
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

`/etc/harbor-satellite/satellite.env` holds `GROUND_CONTROL_URL`, `HARBOR_REGISTRY_URL` and `TOKEN`. After a successful registration the credentials live in `config.json` and the token is not sent again, but the satellite still refuses to start without a `TOKEN` value unless SPIFFE is enabled, so leave it in the file.

### Kubernetes and k3s

See the [k3s reference architecture](guides/k3s-reference-architecture.md) for a satellite at a k3s site, with SPIRE and the runtime mirror.

### Registration

| Method | Use |
|---|---|
| Token | `POST /api/satellites` (or `groundcontrol register satellite`) returns a token valid once for 24 hours. Start the satellite with it. |
| SPIFFE | Register the satellite's identity with `POST /api/satellites/register` and start it with `--spiffe-enabled`. See the [SPIFFE quickstarts](../examples/deploy/spiffe/README.md). |

All flags, variables and `config.json` keys are in the [Satellite Configuration Reference](reference/satellite.md).

## Verify the Installation

```bash
curl -s https://gc.example.com/health                       # {"status":"healthy",...}
curl -s 127.0.0.1:8586/healthz                              # on the satellite host
curl -s -o /dev/null -w '%{http_code}\n' 127.0.0.1:8585/v2/ # 200
```

Then check the satellite in Ground Control: `GET /api/satellites/{satellite}/lifecycle` reports `online` and, after its first replication, `converged`.
