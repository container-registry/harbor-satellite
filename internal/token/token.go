package token

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrTokenExpired     = errors.New("token expired")
	ErrTokenMalformed   = errors.New("token malformed")
	ErrTokenInvalid     = errors.New("token invalid")
	ErrTokenAlreadyUsed = errors.New("token already used")
	ErrTokenRateLimited = errors.New("token rate limited")
)

const (
	// TokenVersion is the current token format version.
	TokenVersion = 1
	// DefaultTokenExpiry is the default token expiration time.
	DefaultTokenExpiry = 24 * time.Hour
	// TokenLength is the random portion length in bytes.
	TokenLength = 32
)

// JoinToken represents a satellite join token for zero-touch registration.
type JoinToken struct {
	Version   int       `json:"v"`
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"exp"`
	GroundURL string    `json:"url,omitempty"`
}

// GenerateToken creates a new join token.
func GenerateToken(groundControlURL string, expiry time.Duration) (*JoinToken, string, error) {
	idBytes := make([]byte, TokenLength)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, "", err
	}

	id := base64.RawURLEncoding.EncodeToString(idBytes)

	token := &JoinToken{
		Version:   TokenVersion,
		ID:        id,
		ExpiresAt: time.Now().Add(expiry),
		GroundURL: groundControlURL,
	}

	encoded, err := token.Encode()
	if err != nil {
		return nil, "", err
	}

	return token, encoded, nil
}

// Encode encodes the token to a string suitable for transmission.
func (t *JoinToken) Encode() (string, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeToken decodes a token string into a JoinToken.
func DecodeToken(tokenStr string) (*JoinToken, error) {
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return nil, ErrTokenMalformed
	}

	data, err := base64.RawURLEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, ErrTokenMalformed
	}

	var token JoinToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, ErrTokenMalformed
	}

	if token.Version == 0 || token.ID == "" {
		return nil, ErrTokenMalformed
	}

	return &token, nil
}

// Validate checks if the token is valid.
func (t *JoinToken) Validate() error {
	if t.Version != TokenVersion {
		return ErrTokenInvalid
	}

	if t.ID == "" {
		return ErrTokenMalformed
	}

	if time.Now().After(t.ExpiresAt) {
		return ErrTokenExpired
	}

	return nil
}

// ValidateForGroundControl checks if token is valid for a specific Ground Control URL.
func (t *JoinToken) ValidateForGroundControl(groundControlURL string) error {
	if err := t.Validate(); err != nil {
		return err
	}

	if t.GroundURL != "" && t.GroundURL != groundControlURL {
		return ErrTokenInvalid
	}

	return nil
}

// IsExpired returns true if the token has expired.
func (t *JoinToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// TimeToExpiry returns the duration until the token expires.
func (t *JoinToken) TimeToExpiry() time.Duration {
	return time.Until(t.ExpiresAt)
}
