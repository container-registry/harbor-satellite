package state

import (
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestBuildTLSTransport_SkipVerifyOnly(t *testing.T) {
	f := &URLStateFetcher{tlsCfg: config.TLSConfig{SkipVerify: true}}

	transport, err := f.buildTLSTransport()
	require.NoError(t, err)
	require.NotNil(t, transport)

	httpTransport, ok := transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, httpTransport.TLSClientConfig.InsecureSkipVerify)
}

func TestBuildTLSTransport_NoTLSSettings(t *testing.T) {
	f := &URLStateFetcher{}

	transport, err := f.buildTLSTransport()
	require.NoError(t, err)
	require.Nil(t, transport)
}
