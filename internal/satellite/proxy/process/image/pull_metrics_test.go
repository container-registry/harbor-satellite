package image_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/process/image"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry/telemetrytest"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestPull_RecordsPullsAndFills(t *testing.T) {
	upstream := newStreamingUpstream(t)
	_, layers := upstream.push(t, "team/app", []byte("measured layer"))
	nop := zerolog.Nop()
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), logger.LoggerKey, &nop))
	t.Cleanup(cancel)
	local, err := store.NewOCIStore(t.TempDir())
	require.NoError(t, err)
	spy := &telemetrytest.Spy{}
	handler, err := image.NewPullWithOptions(ctx, proxy.ModeProxy, local, upstream.remote, image.PullOptions{Streaming: true, Recorder: spy})
	require.NoError(t, err)
	server := httptest.NewServer(proxy.New(handler).Handler())
	t.Cleanup(server.Close)

	status, _, err := getThroughProxy(server.URL + "/v2/team/app/manifests/v1")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	status, _, err = getThroughProxy(server.URL + "/v2/team/app/blobs/" + layers[0].Digest.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	status, _, err = getThroughProxy(server.URL + "/v2/team/app/manifests/missing")
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, status)

	got := spy.Snapshot()
	require.Equal(t, []telemetrytest.Pull{
		{Resource: "manifest", Outcome: "ok"},
		{Resource: "blob", Outcome: "ok"},
		{Resource: "manifest", Outcome: "not_found"},
	}, got.Pulls)
	require.Equal(t, []string{"ok", "ok", "error"}, got.Fills)
}
