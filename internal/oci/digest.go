package oci

import (
	"fmt"

	"github.com/opencontainers/go-digest"
)

// VerifyDigest computes the digest of the provided content and compares it
// against the expected OCI digest string.
func VerifyDigest(content []byte, expectedDigest string) error {
	expected, err := digest.Parse(expectedDigest)
	if err != nil {
		return fmt.Errorf("parse expected digest: %w", err)
	}

	actual := expected.Algorithm().FromBytes(content)
	if actual != expected {
		return fmt.Errorf("digest mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}
