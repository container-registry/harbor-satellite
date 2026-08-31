package proxy_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

func TestParseMode(t *testing.T) {
	mode, err := proxy.ParseMode(" PROXY ")
	require.NoError(t, err)
	require.Equal(t, proxy.ModeProxy, mode)
	require.True(t, mode.AllowsUpstreamPull())

	mode, err = proxy.ParseMode("replica")
	require.NoError(t, err)
	require.Equal(t, proxy.ModeReplica, mode)
	require.False(t, mode.AllowsUpstreamPull())

	_, err = proxy.ParseMode("cache")
	require.Error(t, err)
}
