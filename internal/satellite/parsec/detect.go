//go:build parsec

package parsec

import (
	"fmt"
	"os"

	parsecclient "github.com/parallaxsecond/parsec-client-go/parsec"
)

// Detect attempts to ping the PARSEC daemon at the given socket path.
// Returns true if the daemon is reachable and responding.
// This is a cheap connectivity check, not a full capability probe.
//
// parsec-client-go's NewClientConfig() reads the daemon endpoint from the
// PARSEC_SERVICE_ENDPOINT environment variable, so we set that var to the
// caller-supplied path before constructing the client. Without this the
// socketPath argument is silently ignored.
func Detect(socketPath string) bool {
	if socketPath != "" {
		if err := os.Setenv("PARSEC_SERVICE_ENDPOINT", "unix:"+socketPath); err != nil {
			return false
		}
	}
	cfg := parsecclient.NewClientConfig()
	c, err := parsecclient.CreateConfiguredClient(cfg)
	if err != nil {
		return false
	}
	defer c.Close() //nolint:errcheck
	_, _, err = c.Ping()
	return err == nil
}

// MustDetect calls Detect and returns an error if the daemon is unreachable.
// Use this at startup when --parsec-enabled is set.
func MustDetect(socketPath string) error {
	if !Detect(socketPath) {
		return fmt.Errorf("parsec daemon not reachable at %s: ensure the parsec service is running", socketPath)
	}
	return nil
}
