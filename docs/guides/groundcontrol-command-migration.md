# Ground Control Command Migration

Ground Control now ships one executable, `groundcontrol`. Administration commands
retain their existing syntax. Start the server explicitly with `groundcontrol serve`.

| Previous command | Current command |
| --- | --- |
| `ground-control` | `groundcontrol serve` |
| `groundcontrol get satellites` | `groundcontrol get satellites` |
| `go run ./cmd/groundcontrol/server` | `go run ./cmd/groundcontrol serve` |
| `go install ./cmd/groundcontrol/cli` | `go install ./cmd/groundcontrol` |
| `task _build:ground-control` or `task _build:groundcontrol-cli` | `task _build:groundcontrol` |

Install a released version with:

```sh
go install github.com/container-registry/harbor-satellite/cmd/groundcontrol@<version>
groundcontrol serve
```

Replace `<version>` with the release tag. Migrations are embedded in the binary,
so running the installed executable does not require a source checkout. The
optional `/migrations` directory remains available for container deployments.

Release archives are named `groundcontrol_<version>_<os>_<arch>.tar.gz`; the
Linux package name is `groundcontrol`. Update package installation scripts and
service units to invoke `groundcontrol serve`. Satellite release archives now
contain the `satellite` executable, matching `go install` and local builds.

Container image names remain `registry.goharbor.io/harbor-satellite/ground-control`.
Pass `serve` after the image name in `docker run`, use `command: ["serve"]` in
Compose, and `args: ["serve"]` in Kubernetes. Without a command, the image displays
administration help. Repository Compose files and the Helm deployment include
the argument. The chart directory is now `examples/deploy/helm/groundcontrol`;
its chart metadata, release names, and resource naming helpers are unchanged.

`GROUND_CONTROL_*` environment variables, Satellite `--ground-control-*` flags,
JSON/API fields, service and image names, audit component values, and SPIFFE IDs
remain unchanged. Client URL, token, session, and configuration flags apply to
administration commands; `serve` loads server environment configuration instead.

Generation uses `task generate:groundcontrol` and `spec/groundcontrol`. Live reload
uses `air -c .air.groundcontrol.toml`. SPIFFE example directories previously named
`external/gc` are now `external/groundcontrol`.
