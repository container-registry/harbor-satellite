package authn

import (
	"crypto/sha256"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

const (
	SessionTokenPrefix = "hss_"
	MaxSessionCacheTTL = 5 * time.Minute // bound of revocation latency across replicas
)

// NewSessionToken returns "hss_" + base64url(32 random bytes), 47
// characters, and its SHA-256.
func NewSessionToken() (token string, hash [sha256.Size]byte, err error) {
	return "", [sha256.Size]byte{}, notimpl.Err("4.1 new session token")
}

// HashSessionToken is the SHA-256 of a session token; anything without the
// prefix and length of one is fleet.ErrSessionInvalid, so a robot secret
// or a user session never reaches the lookup.
func HashSessionToken(token string) ([sha256.Size]byte, error) {
	return [sha256.Size]byte{}, notimpl.Err("4.1 hash session token")
}
