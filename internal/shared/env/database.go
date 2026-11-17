package env

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// ConnectionURL builds postgres://user:pass@host:port/db with escaped
// credentials and the sslmode and sslrootcert query parameters.
func (d Database) ConnectionURL() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	query := url.Values{"sslmode": {d.SSLMode}}
	if d.SSLRootCert != "" {
		query.Set("sslrootcert", d.SSLRootCert)
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.Username, d.Password),
		Host:     net.JoinHostPort(d.Host, d.Port),
		Path:     "/" + d.Database,
		RawQuery: query.Encode(),
	}
	return u.String(), nil
}

// Validate rejects an SSLMode that libpq does not know.
func (d Database) Validate() error {
	for _, mode := range sslModes {
		if d.SSLMode == mode {
			return nil
		}
	}
	if d.SSLMode == "" {
		return errors.New("DB_SSLMODE is empty; use one of " + strings.Join(sslModes, ", "))
	}
	return fmt.Errorf("DB_SSLMODE %q is not one of %s", d.SSLMode, strings.Join(sslModes, ", "))
}
