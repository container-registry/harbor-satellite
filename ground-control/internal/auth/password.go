package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/container-registry/harbor-satellite/ground-control/pkg/crypto"
)

// HashPassword creates an Argon2id hash of the password.
func HashPassword(password string) (string, error) {
	return crypto.HashSecret(password)
}

// VerifyPassword compares a password against an Argon2id hash.
func VerifyPassword(password, encodedHash string) (bool, error) {
	// The crypto package returns bool directly without error for simplicity.
	// Wrap it to maintain the (bool, error) signature for backward compatibility.
	return crypto.VerifySecret(password, encodedHash), nil
}

// GenerateSessionToken creates a cryptographically random session token.
func GenerateSessionToken() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
