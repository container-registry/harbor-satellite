package proxy_test

import (
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

func TestWhenAppliesOnlyToMatchingContracts(t *testing.T) {
	t.Parallel()

	applyCount := 0
	selected := proxy.LayerFunc(func(next proxy.Handler) proxy.Handler {
		return proxy.HandlerFunc(func(contract *proxy.Contract) error {
			applyCount++
			return next.Handle(contract)
		})
	})
	identity := proxy.HandlerFunc(func(*proxy.Contract) error { return nil })
	chain := proxy.When(proxy.IsPull, selected).Wrap(identity)

	require.NoError(t, chain.Handle(&proxy.Contract{Operation: proxy.Blob, Access: proxy.ReadAccess}))
	require.NoError(t, chain.Handle(&proxy.Contract{Operation: proxy.Tags, Access: proxy.ReadAccess}))
	require.Equal(t, 1, applyCount)
}

func TestPredicates(t *testing.T) {
	t.Parallel()

	pullBlob := &proxy.Contract{Operation: proxy.Blob, Access: proxy.ReadAccess}
	pushManifest := &proxy.Contract{Operation: proxy.Manifest, Access: proxy.WriteAccess}

	require.True(t, proxy.IsPull(pullBlob))
	require.True(t, proxy.IsBlob(pullBlob))
	require.False(t, proxy.IsManifest(pullBlob))
	require.False(t, proxy.IsPull(pushManifest))
	require.True(t, proxy.IsManifest(pushManifest))
	require.True(t, proxy.Not(proxy.IsPull)(pushManifest))
}

func TestNotPreservesNilForConfigurationValidation(t *testing.T) {
	t.Parallel()
	require.Nil(t, proxy.Not(nil))
}
