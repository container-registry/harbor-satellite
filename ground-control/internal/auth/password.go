package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP 2024 recommended)
const (
	argonMemory      = 19456 // 19 MiB
	argonIterations  = 2
	argonParallelism = 1
	argonSaltLength  = 16
	argonKeyLength   = 32
)

var (
	ErrInvalidHash         = errors.New("invalid hash format")
	ErrIncompatibleVersion = errors.New("incompatible argon2 version")
)

// HashPassword creates an Argon2id hash of the password
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemory,
		argonParallelism,
		argonKeyLength,
	)

	// Encode as: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		b64Salt,
		b64Hash,
	), nil
}

// VerifyPassword compares a password against an Argon2id hash
func VerifyPassword(password, encodedHash string) (bool, error) {
	salt, hash, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	otherHash := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemory,
		argonParallelism,
		argonKeyLength,
	)

	// Constant-time comparison
	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

func decodeHash(encodedHash string) (salt, hash []byte, err error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return nil, nil, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return nil, nil, ErrInvalidHash
	}

	var version int
	_, err = fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return nil, nil, ErrIncompatibleVersion
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, ErrInvalidHash
	}

	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, ErrInvalidHash
	}

	return salt, hash, nil
}

// GenerateSessionToken creates a cryptographically random session token
func GenerateSessionToken() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
