# Harbor Satellite Quickstart

Pick how satellites authenticate to Ground Control, then follow that guide.

## Without SPIFFE (Token-based ZTR)

Ground Control issues a registration token for each satellite. The token is single-use and expires after 24 hours. No SPIRE infrastructure is needed, which makes this the simplest setup for development and testing.

See the [token-based quickstart](no-spiffe/quickstart.md).

## With SPIFFE (Zero-Trust Identity)

SPIFFE/SPIRE provides cryptographic identity, mTLS and automatic certificate rotation between satellites and Ground Control.

| Attestation Method | Description | Guide |
|-------------------|-------------|-------|
| Join Token | One-time tokens, simplest setup | [spiffe/join-token/](spiffe/join-token/) |
| X.509 PoP | Pre-provisioned X.509 certificates | [spiffe/x509pop/](spiffe/x509pop/) |
| SSH PoP | SSH host certificates | [spiffe/sshpop/](spiffe/sshpop/) |

Each method guide covers the method-specific setup. The [SPIFFE/SPIRE quickstart](spiffe/README.md) holds the shared parts: architecture, environment variables, login, config and group assignment, verification, cleanup, troubleshooting and embedded SPIRE status.

## Helm

[`helm/ground-control/`](helm/ground-control/) deploys Ground Control and PostgreSQL to Kubernetes. Set `harbor.url`, `harbor.password`, `adminPassword` and `database.password`; the chart refuses to render without the three passwords. See `values.yaml` for all settings:

```bash
helm install ground-control examples/deploy/helm/ground-control \
  --set harbor.url=https://harbor.example.com \
  --set harbor.password=<harbor-password> \
  --set adminPassword=<admin-password> \
  --set database.password=<db-password>
```

## Where Replicated Images Go

By default the satellite writes replicated images into an OCI image layout at `<config-dir>/oci` (override with `--registry-data-dir` or `REGISTRY_DATA_DIR`). The satellite does not serve this layout over HTTP, so there is no registry endpoint to `docker pull` from and it cannot act as a CRI mirror.

To pull images from the satellite, run it with a bring-your-own registry (`--byo-registry --registry-url <url>`). The satellite then pushes replicated images into that registry, and clients and CRI mirrors pull from it. The quickstarts use the default OCI layout and verify replication by inspecting the layout on disk.

## Host Ports

SPIFFE quickstarts (`spiffe/*/external/`):

| Component | Host port | Container port | Override |
|-----------|-----------|----------------|----------|
| Ground Control (HTTPS) | 9080 | 8080 | `GC_HOST_PORT` |
| SPIRE Server | 9081 | 8081 | `SPIRE_HOST_PORT` |
| PostgreSQL | not exposed | 5432 | |
| Satellite | not exposed | none | |

Token-based quickstart (`docker-compose.yml` in the repository root):

| Component | Host port | Container port | Override |
|-----------|-----------|----------------|----------|
| Ground Control (HTTP) | 8080 | 8080 | `GC_HOST_PORT` |
| PostgreSQL | 8100 | 5432 | |
| Satellite | not exposed | none | |
