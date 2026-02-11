package token

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryTokenStore_MarkUsed(t *testing.T) {
	t.Run("marks token as used", func(t *testing.T) {
		store := NewMemoryTokenStore(5, time.Minute)

		err := store.MarkUsed("token-1")
		require.NoError(t, err)

		require.True(t, store.IsUsed("token-1"))
	})

	t.Run("token single use enforced", func(t *testing.T) {
		store := NewMemoryTokenStore(5, time.Minute)

		err := store.MarkUsed("token-1")
		require.NoError(t, err)

		err = store.MarkUsed("token-1")
		require.ErrorIs(t, err, ErrTokenAlreadyUsed)
	})

	t.Run("different tokens can be used", func(t *testing.T) {
		store := NewMemoryTokenStore(5, time.Minute)

		err := store.MarkUsed("token-1")
		require.NoError(t, err)

		err = store.MarkUsed("token-2")
		require.NoError(t, err)

		require.True(t, store.IsUsed("token-1"))
		require.True(t, store.IsUsed("token-2"))
	})
}

func TestMemoryTokenStore_IsUsed(t *testing.T) {
	t.Run("returns false for unused token", func(t *testing.T) {
		store := NewMemoryTokenStore(5, time.Minute)
		require.False(t, store.IsUsed("unused-token"))
	})

	t.Run("returns true for used token", func(t *testing.T) {
		store := NewMemoryTokenStore(5, time.Minute)
		require.NoError(t, store.MarkUsed("used-token"))
		require.True(t, store.IsUsed("used-token"))
	})
}

func TestMemoryTokenStore_RateLimit(t *testing.T) {
	t.Run("allows attempts within limit", func(t *testing.T) {
		store := NewMemoryTokenStore(3, time.Minute)

		for i := 0; i < 3; i++ {
			err := store.CheckRateLimit("192.168.1.1")
			require.NoError(t, err)
			store.RecordAttempt("192.168.1.1")
		}
	})

	t.Run("blocks attempts over limit", func(t *testing.T) {
		store := NewMemoryTokenStore(3, time.Minute)

		for i := 0; i < 3; i++ {
			store.RecordAttempt("192.168.1.1")
		}

		err := store.CheckRateLimit("192.168.1.1")
		require.ErrorIs(t, err, ErrTokenRateLimited)
	})

	t.Run("rate limits are per IP", func(t *testing.T) {
		store := NewMemoryTokenStore(3, time.Minute)

		for i := 0; i < 3; i++ {
			store.RecordAttempt("192.168.1.1")
		}

		err := store.CheckRateLimit("192.168.1.2")
		require.NoError(t, err)
	})
}

func TestMemoryTokenStore_Cleanup(t *testing.T) {
	store := NewMemoryTokenStore(5, 10*time.Millisecond)

	store.RecordAttempt("192.168.1.1")
	store.RecordAttempt("192.168.1.1")

	time.Sleep(20 * time.Millisecond)

	store.Cleanup()

	err := store.CheckRateLimit("192.168.1.1")
	require.NoError(t, err)
}

func TestMockTokenStore(t *testing.T) {
	t.Run("mark used works", func(t *testing.T) {
		store := NewMockTokenStore()

		err := store.MarkUsed("token-1")
		require.NoError(t, err)

		err = store.MarkUsed("token-1")
		require.ErrorIs(t, err, ErrTokenAlreadyUsed)
	})

	t.Run("is used works", func(t *testing.T) {
		store := NewMockTokenStore()

		require.False(t, store.IsUsed("token-1"))

		require.NoError(t, store.MarkUsed("token-1"))

		require.True(t, store.IsUsed("token-1"))
	})

	t.Run("rate limit can be configured", func(t *testing.T) {
		store := NewMockTokenStore()
		store.RateLimited["192.168.1.1"] = true

		err := store.CheckRateLimit("192.168.1.1")
		require.ErrorIs(t, err, ErrTokenRateLimited)

		err = store.CheckRateLimit("192.168.1.2")
		require.NoError(t, err)
	})
}
