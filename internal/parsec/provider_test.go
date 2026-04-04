//go:build parsec

package parsec

import (
	"crypto"
	"testing"
	harborcrypto "github.com/container-registry/harbor-satellite/internal/crypto"
)

func TestKeyRef(t *testing.T) {
	expectedName := "test-identity-key"
	var privKey crypto.PrivateKey = KeyRef{Name: expectedName}

	ref, ok := privKey.(KeyRef)
	if !ok {
		t.Fatalf("Expected privKey to be of type KeyRef, but it was %T", privKey)
	}

	if ref.Name != expectedName {
		t.Errorf("Expected key name %q, got %q", expectedName, ref.Name)
	}
}

func TestSignerInterface(t *testing.T) {
	var _ crypto.Signer = (*Signer)(nil)
}

func TestProviderInterface(t *testing.T) {
	var _ harborcrypto.Provider = (*KeyProvider)(nil)
}