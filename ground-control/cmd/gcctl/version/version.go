// Package version holds build-time version info for gcctl.
//
// These variables are populated via -ldflags at build time:
//
//	go build -ldflags "-X .../version.Version=v1.0.0 -X .../version.GitCommit=abc123"
package version

var (
	// Version is the semantic version (set via ldflags).
	Version = "dev"
	// GitCommit is the git commit hash (set via ldflags).
	GitCommit = "unknown"
	// BuildDate is the build timestamp (set via ldflags).
	BuildDate = "unknown"
)
