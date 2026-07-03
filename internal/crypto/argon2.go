package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

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
	parts := splitHash(storedHash)
	if len(parts) != 6 {
		return false
	}

	// Parse version
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false
	}
	if version != argon2.Version {
		return false
	}

	// Parse parameters (memory, time, parallelism)
	var memory, time uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &parallelism); err != nil {
		return false
	}

	// Decode salt
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	// Decode stored hash to get key length
	storedHashBytes, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	keyLen := uint32(len(storedHashBytes))

	// Recompute hash using parameters from the stored hash
	hash := argon2.IDKey([]byte(secret), salt, time, memory, parallelism, keyLen)
	return subtle.ConstantTimeCompare(storedHashBytes, hash) == 1
}

func splitHash(hash string) []string {
	return strings.Split(hash, "$")
}
