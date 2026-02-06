package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	ArgonTime        = 2
	ArgonMemory      = 19456 // 19 MiB
	ArgonParallelism = 1
	ArgonSaltSize    = 16 // bytes
	ArgonKeySize     = 32 // bytes
)

// HashSecret computes an argon2id hash of the secret using a random salt.
func HashSecret(secret string) (string, error) {
	salt := make([]byte, ArgonSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(secret), salt, ArgonTime, ArgonMemory, ArgonParallelism, ArgonKeySize)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, ArgonMemory, ArgonTime, ArgonParallelism, b64Salt, b64Hash), nil
}

// VerifySecret checks if the given secret matches the stored hash.
func VerifySecret(secret, storedHash string) bool {
	// Parse the hash format: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
	// For simplicity, we use the hardcoded parameters from constants.
	// A more robust implementation would parse parameters from the hash string.
	parts := splitHash(storedHash)
	if len(parts) != 6 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	hash := argon2.IDKey([]byte(secret), salt, ArgonTime, ArgonMemory, ArgonParallelism, ArgonKeySize)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return subtle.ConstantTimeCompare([]byte(b64Hash), []byte(parts[5])) == 1
}

func splitHash(hash string) []string {
	var parts []string
	var current string
	for _, c := range hash {
		if c == '$' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	parts = append(parts, current)
	return parts
}
