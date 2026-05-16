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

const (
	// DefaultSocketPath is the standard PARSEC daemon socket location.
	DefaultSocketPath = "/run/parsec/parsec.sock"

	// identityKeyName is the hardware-resident key used for signing (CSR, mTLS).
	// The private key material NEVER leaves the secure element.
	identityKeyName = "satellite-identity-key"

	// configSealKeyName is the hardware-resident symmetric key used to seal
	// the satellite config at rest. Replaces the software device-fingerprint approach.
	configSealKeyName = "satellite-config-seal-key"
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
