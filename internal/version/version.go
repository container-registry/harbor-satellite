// Package version exposes build-time metadata that is stamped into binaries
// via Go linker -X flags in taskfiles/build.yml and .goreleaser.yaml.
//
// The variables are package-level vars (not consts) so the linker can
// overwrite them at link time:
//
//	-X github.com/container-registry/harbor-satellite/internal/version.Version=v1.2.3
//	-X github.com/container-registry/harbor-satellite/internal/version.GitCommit=abc1234
package version

// Version is the release tag (e.g. "v1.2.3") stamped at build time.
// Falls back to "dev" when the binary is built without ldflags.
var Version = "dev"

// GitCommit is the full Git commit SHA stamped at build time.
// Falls back to "unknown" when the binary is built without ldflags.
var GitCommit = "unknown"
