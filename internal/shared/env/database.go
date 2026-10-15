package env

import "github.com/container-registry/harbor-satellite/internal/shared/notimpl"

// ConnectionURL builds postgres://user:pass@host:port/db with escaped
// credentials and the sslmode and sslrootcert query parameters. URL()
// delegates to it once it is implemented.
func (d Database) ConnectionURL() (string, error) {
	return "", notimpl.Err("3.3 connection URL with TLS")
}

// Validate rejects an SSLMode that libpq does not know.
func (d Database) Validate() error {
	return notimpl.Err("3.3 database settings validation")
}
