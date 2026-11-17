package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

const (
	SessionTokenPrefix = "hss_"
	MaxSessionCacheTTL = 5 * time.Minute // bound of revocation latency across replicas
)

const (
	sessionTokenBytes = 32
	sessionTokenLen   = len(SessionTokenPrefix) + 43
)

// NewSessionToken returns "hss_" + base64url(32 random bytes), 47
// characters, and its SHA-256.
func NewSessionToken() (token string, hash [sha256.Size]byte, err error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("session token: %w", err)
	}
	token = SessionTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}

// HashSessionToken is the SHA-256 of a session token; anything without the
// prefix and length of one is fleet.ErrSessionInvalid, so a robot secret
// or a user session never reaches the lookup.
func HashSessionToken(token string) ([sha256.Size]byte, error) {
	if len(token) != sessionTokenLen || !strings.HasPrefix(token, SessionTokenPrefix) {
		return [sha256.Size]byte{}, fleet.ErrSessionInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token[len(SessionTokenPrefix):])
	if err != nil || len(raw) != sessionTokenBytes {
		return [sha256.Size]byte{}, fleet.ErrSessionInvalid
	}
	return sha256.Sum256([]byte(token)), nil
}
