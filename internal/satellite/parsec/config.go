// Package parsec provides optional hardware-backed cryptography for Harbor Satellite
// via the CNCF PARSEC service (Platform AbstRaction for SECurity).
//
// PARSEC abstracts TPM 2.0, ARM TrustZone, Intel SGX, and PKCS#11 HSMs behind a
// single Unix socket API, so this package has no compile-time dependency on any
// specific hardware library.
//
// Build tags:
//
//	parsec  — include PARSEC support (requires the parsec daemon at runtime)
//	default — stub implementation; all operations return ErrParsecNotAvailable
//
// The PARSEC daemon is external infrastructure (like SPIRE). When --parsec-enabled
// is set, the satellite pings the socket at startup and halts if unreachable.
package parsec

import (
	"fmt"
	"path/filepath"
)

const (
	// DefaultSocketPath is the standard PARSEC daemon socket location.
	DefaultSocketPath = "/run/parsec/parsec.sock"
)

// Config holds PARSEC integration configuration.
type Config struct {
	Enabled    bool   `json:"enabled,omitempty"`
	SocketPath string `json:"socket_path,omitempty"`
}

// DefaultConfig returns safe defaults: disabled, standard socket path.
func DefaultConfig() Config {
	return Config{
		Enabled:    false,
		SocketPath: DefaultSocketPath,
	}
}

// Validate checks that the configuration is internally consistent. Called from
// pkg/config/validate.go so misconfiguration surfaces at startup rather than
// at the first PARSEC operation.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.SocketPath == "" {
		return fmt.Errorf("parsec: socket_path must not be empty when enabled (default: %q)", DefaultSocketPath)
	}
	if !filepath.IsAbs(c.SocketPath) {
		return fmt.Errorf("parsec: socket_path must be absolute, got %q", c.SocketPath)
	}

	return nil
}
