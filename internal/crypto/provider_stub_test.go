//go:build nospiffe

package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The nospiffe build ships no cryptography. Every operation must report that
// instead of returning a value that looks like it worked: the previous stub
// returned plaintext from Encrypt, nil (success) from Verify and zeros from
// RandomBytes, so a caller could write "encrypted" credentials that were
// nothing of the sort and trust signatures that were never checked.
func TestUnavailableProvider_FailsClosed(t *testing.T) {
	p := NewDefaultProvider()
	secret := []byte("harbor-robot-credentials")

	t.Run("encrypt refuses instead of returning plaintext", func(t *testing.T) {
		out, err := p.Encrypt(secret, []byte("key"))
		require.ErrorIs(t, err, ErrCryptoUnavailable)
		require.Nil(t, out, "no data may be returned when nothing was encrypted")
		require.NotEqual(t, secret, out, "the plaintext must never be handed back as ciphertext")
	})

	t.Run("decrypt refuses", func(t *testing.T) {
		out, err := p.Decrypt(secret, []byte("key"))
		require.ErrorIs(t, err, ErrCryptoUnavailable)
		require.Nil(t, out)
	})

	t.Run("verify refuses instead of reporting success", func(t *testing.T) {
		err := p.Verify(secret, []byte("any-signature"), nil)
		require.ErrorIs(t, err, ErrCryptoUnavailable, "an unchecked signature must never be reported as valid")
	})

	t.Run("sign refuses", func(t *testing.T) {
		out, err := p.Sign(secret, nil)
		require.ErrorIs(t, err, ErrCryptoUnavailable)
		require.Nil(t, out)
	})

	t.Run("random bytes refuses instead of returning zeros", func(t *testing.T) {
		out, err := p.RandomBytes(16)
		require.ErrorIs(t, err, ErrCryptoUnavailable)
		require.Nil(t, out, "a predictable all-zero salt must never be produced")
	})

	t.Run("derive key refuses", func(t *testing.T) {
		out, err := p.DeriveKey(secret, []byte("salt"), 32)
		require.ErrorIs(t, err, ErrCryptoUnavailable)
		require.Nil(t, out)
	})

	t.Run("generate key pair refuses", func(t *testing.T) {
		priv, pub, err := p.GenerateKeyPair()
		require.ErrorIs(t, err, ErrCryptoUnavailable)
		require.Nil(t, priv)
		require.Nil(t, pub)
	})

	t.Run("hash does not echo its input", func(t *testing.T) {
		require.Nil(t, p.Hash(secret), "returning the input unchanged would be an identity function named Hash")
	})
}

// EncryptionAvailable is what callers check before persisting secrets, so the
// nospiffe build has to report false.
func TestUnavailableProvider_EncryptionUnavailable(t *testing.T) {
	require.False(t, EncryptionAvailable, "the nospiffe build cannot encrypt and must not claim otherwise")
}

// The stub must satisfy Provider so it can be swapped in for the real one.
func TestUnavailableProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = NewUnavailableProvider()
	var _ Provider = NewDefaultProvider()
}
