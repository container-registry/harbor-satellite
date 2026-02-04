package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// Argon2id parameters (OWASP 2024 recommended)
const (
	argonMemory      = 19456 // 19 MiB
	argonIterations  = 2
	argonParallelism = 1
	argonSaltLength  = 16
	argonKeyLength   = 32
)

// PBKDF2-SHA256 parameters (FIPS-approved, OWASP 2024 recommended)
const (
	pbkdf2Iterations = 210000
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
)

// FIPSMode enables FIPS 140-2 compliant cryptography when true.
// When enabled, new passwords are hashed with PBKDF2-SHA256 instead of Argon2id.
var FIPSMode bool

var (
	ErrInvalidHash         = errors.New("invalid hash format")
	ErrIncompatibleVersion = errors.New("incompatible argon2 version")
)

// HashPassword creates a password hash using the appropriate algorithm.
// In FIPS mode, uses PBKDF2-SHA256. Otherwise, uses Argon2id.
func HashPassword(password string) (string, error) {
	if FIPSMode {
		return hashPasswordPBKDF2(password)
	}
	return hashPasswordArgon2id(password)
}

// hashPasswordPBKDF2 creates a PBKDF2-SHA256 hash of the password (FIPS-approved).
func hashPasswordPBKDF2(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLength, sha256.New)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$pbkdf2-sha256$i=%d$%s$%s", pbkdf2Iterations, b64Salt, b64Hash), nil
}

// hashPasswordArgon2id creates an Argon2id hash of the password.
func hashPasswordArgon2id(password string) (string, error) {
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

type hashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	keyLength   uint32
	salt        []byte
	hash        []byte
}

// VerifyPassword compares a password against a hash, auto-detecting the algorithm.
func VerifyPassword(password, encodedHash string) (bool, error) {
	if strings.HasPrefix(encodedHash, "$pbkdf2-sha256$") {
		return verifyPBKDF2(password, encodedHash)
	}
	if strings.HasPrefix(encodedHash, "$argon2id$") {
		return verifyArgon2id(password, encodedHash)
	}
	return false, ErrInvalidHash
}

// verifyPBKDF2 verifies a password against a PBKDF2-SHA256 hash.
func verifyPBKDF2(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 5 {
		return false, ErrInvalidHash
	}

	var iterations int
	_, err := fmt.Sscanf(parts[2], "i=%d", &iterations)
	if err != nil || iterations <= 0 {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, ErrInvalidHash
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}

	otherHash := pbkdf2.Key([]byte(password), salt, iterations, len(hash), sha256.New)

	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

// verifyArgon2id verifies a password against an Argon2id hash.
func verifyArgon2id(password, encodedHash string) (bool, error) {
	params, err := decodeArgon2idHash(encodedHash)
	if err != nil {
		return false, err
	}

	otherHash := argon2.IDKey(
		[]byte(password),
		params.salt,
		params.iterations,
		params.memory,
		params.parallelism,
		params.keyLength,
	)

	if subtle.ConstantTimeCompare(params.hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

func decodeArgon2idHash(encodedHash string) (*hashParams, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return nil, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return nil, ErrInvalidHash
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return nil, ErrIncompatibleVersion
	}

	var memory, iterations uint32
	var parallelism uint8
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return nil, ErrInvalidHash
	}

	if iterations == 0 || parallelism == 0 || memory == 0 {
		return nil, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, ErrInvalidHash
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, ErrInvalidHash
	}

	return &hashParams{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		keyLength:   uint32(len(hash)),
		salt:        salt,
		hash:        hash,
	}, nil
}

// NeedsRehash returns true if the hash should be migrated to the current algorithm.
// In FIPS mode, Argon2id hashes need to be migrated to PBKDF2-SHA256.
func NeedsRehash(encodedHash string) bool {
	if FIPSMode && strings.HasPrefix(encodedHash, "$argon2id$") {
		return true
	}
	return false
}

// GenerateSessionToken creates a cryptographically random session token.
func GenerateSessionToken() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

