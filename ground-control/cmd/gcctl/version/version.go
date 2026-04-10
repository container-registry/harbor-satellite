// Package version holds build-time version info for gcctl.
//
// These variables are populated via -ldflags at build time:
//
//	go build -ldflags "-X .../version.Version=v1.0.0 -X .../version.GitCommit=abc123"
package version

// These variables are intentionally package-level so that go build -ldflags
// can inject values at build time. The gochecknoglobals linter exception
// is warranted here — there is no other mechanism for ldflags injection.
//
//nolint:gochecknoglobals
var (
	// Version is the semantic version (set via ldflags).
	Version = "dev"
	// GitCommit is the git commit hash (set via ldflags).
	GitCommit = "unknown"
	// BuildDate is the build timestamp (set via ldflags).
	BuildDate = "unknown"
)
