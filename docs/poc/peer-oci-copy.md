# Peer OCI copy PoC

Pull-based copy between default-mode satellites (ORAS OCI layout). Each satellite
**is** the ADR-0009 replica proxy on `--registry-listen`: GET/HEAD from the local
layout, writes 405. There is no second facade server.

`PEER_URLS` / `--peers` is a **static same-group allow-list**. List only replica-proxy
URLs of satellites in the same Ground Control group. Ground Control groups are image
lists; they do not publish peer listen URLs this term.

## Maintainer points

- **Pull, not push.** B pulls; A does not push.
- **Concurrent first-hit.** B `Resolve`s the digest on every peer at once; the first
  success wins; the rest are cancelled.
- **Dead peer.** Skip locally and try the next peer, then Harbor. Do not wait for
  Ground Control in the copy path (`/sat/sync` status report currently 404s).
- **Harbor destination refs.** Peer copy tags the layout with the Harbor name, never
  the peer hostname.
- **Flags/env only.** Listen and peer URLs are not in `config.json`.
  `--peer-listen` / `PEER_LISTEN` remain aliases of `--registry-listen` /
  `REGISTRY_LISTEN`.

## Non-goals

- Spegel / DHT
- Push-based distribution
- Full ADR-0009 proxy (forward to Harbor + policy)
- Ground Control peer-IP API
- `REACHOUT_SATS=global` (cross-group)

## MicroVM testbed

Isolated-node (kill-A) runs use NixOS MicroVMs on qemu user-net. Same protocol;
see [test/microvm/README.md](../../test/microvm/README.md).

## Demo layout

Two compose services, **same Ground Control group**:

| Service | Role | Listen | Peers |
|---|---|---|---|
| `satellite-a` | Seed from Harbor | `:5000` (host 5000) | none |
| `satellite-b` | Pull from A, else Harbor | `:5000` (host 5001) | `http://satellite-a:5000` |

The default `satellite` service is on profile `single` and must stay off.

Ground Control on the host is **http://127.0.0.1:7080** (`GC_PORT`, maps to
container 8080). `GET /` is 404; health is `GET /ping`. GC login is
**admin / Harbor12345**, not the Harbor robot user.

## ZTR tokens (read this first)

Tokens from `POST /api/satellites` are **one-shot**. Starting a container consumes
the token. Common failures:

| Symptom | Cause |
|---|---|
| `username is empty` | ZTR never wrote robot creds (token missing, already spent, or `.env` not saved) |
| `400 Bad Request` on ZTR | Invalid or already-used token |
| `429` every ~5s | Spent token, container still retrying — **stop the satellite** before minting another |
| Wrong satellite, Harbor robots clash | Re-registering `satellite-a` / `satellite-b` without deleting Harbor robots `robot_satellite-a` / `robot_satellite-b` |

Rules:

1. Create both GC satellites in the **same group**, copy tokens, **write `TOKEN_A` and
   `TOKEN_B` into `.env` and save the file** before any `up` of A or B.
2. Tokens are 32 hex characters. Compare the **first 8 characters** to the API
   response. Do not assume any 32-character placeholder is the new token.
3. Never `up --force-recreate` A or B. That burns the token again.
4. Do not `docker compose up -d` the whole project. Profiles `peer-a` / `peer-b` exist
   so A and B cannot start together by accident.

## Run

Compose reads `.env` from the repo root. Set Harbor (`HARBOR_URL`,
`HARBOR_USERNAME`, `HARBOR_PASSWORD`) and `HARBOR_REGISTRY_URL` (default
`https://demo.goharbor.io`) there first.

```bash
COMPOSE="docker compose -f docker-compose.yml -f docker-compose.peer-poc.yml"

# 1. Ground Control only. Do not start satellites yet.
$COMPOSE up -d postgres ground-control
curl -sf http://127.0.0.1:7080/ping   # pong
```

Log in, create one group, one config (`bring_own_registry: false`), then two
satellites in that group (`satellite-a`, `satellite-b`). Save the returned tokens:

```bash
curl -s http://127.0.0.1:7080/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Harbor12345"}'
# Use token with: -H "Authorization: Bearer <session>"
```

Write `TOKEN_A` and `TOKEN_B` into `.env`. Save the file. Confirm the first 8
characters. Then:

```bash
# 2. Seed A from Harbor. Wait until it has content.
$COMPOSE --profile peer-a up -d --no-deps satellite-a
docker logs satellite-a 2>&1 | grep -E 'Replica proxy listening|Artifact replicated to OCI store'

# 3. Only now start B. It must be in the same group as A.
$COMPOSE --profile peer-b up -d --no-deps satellite-b
docker logs satellite-b 2>&1 | grep -E 'Replica proxy listening|Artifact copied from peer|falling back to Harbor'
```

`docker logs ... | grep` without `2>&1` misses satellite stderr.

To re-demo peer copy without burning A's token, wipe **only B's volume**:

```bash
$COMPOSE --profile peer-b stop satellite-b
docker volume rm "${COMPOSE_PROJECT_NAME:-$(basename "$PWD")}_satellite-b-data"
$COMPOSE --profile peer-b up -d --no-deps satellite-b
```

## Expected logs

**A (Harbor seed)**

```
Replica proxy listening
Artifact replicated to OCI store
```

**B (peer hit)**

```
Replica proxy listening
Artifact copied from peer
```

The `reference` field is the Harbor dest (for example `demo.goharbor.io/...`), not
`satellite-a:5000/...`.

**Ignore** (not peer-copy failures): `refresh_credentials` → `/sat/refresh` 404;
`Failed to extract logger from context`.

## Kill-A fallback

Stop A, reset B's layout, start B again. A is unreachable, so B skips the peer
and copies from Harbor:

```bash
$COMPOSE --profile peer-a stop satellite-a
$COMPOSE --profile peer-b stop satellite-b
docker volume rm "${COMPOSE_PROJECT_NAME:-$(basename "$PWD")}_satellite-b-data"
$COMPOSE --profile peer-b up -d --no-deps satellite-b
docker logs satellite-b 2>&1 | grep -E 'No peer has artifact, falling back to Harbor|Artifact replicated to OCI store'
```

Bring A back with a **new** ZTR token if the original was already consumed
(`TOKEN_A` in `.env`, then `--profile peer-a up -d --no-deps satellite-a`).
Do not force-recreate on the spent token.

## Flags

| Flag / env | Meaning |
|---|---|
| `--registry-listen` / `REGISTRY_LISTEN` | Bind this node's replica proxy (empty disables it) |
| `--peer-listen` / `PEER_LISTEN` | Deprecated alias of registry-listen |
| `--peers` / `PEER_URLS` | Comma-separated same-group replica-proxy URLs |

Unit coverage: `go test ./internal/satellite/peer/ ./internal/satellite/store/ ./internal/satellite/state/ ./internal/shared/env/`
