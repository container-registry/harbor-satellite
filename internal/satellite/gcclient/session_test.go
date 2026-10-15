package gcclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// A renewal from a heartbeat response must apply to the next request
// without building a new client.
func TestSessionBearer_ReadsTokenPerRequest(t *testing.T) {
	token := "hss_first"
	creds := gcclient.SessionBearer(func() string { return token })

	first := httptest.NewRequest(http.MethodPost, "https://gc.test/satellites/sync", nil)
	creds.Authorize(first)
	token = "hss_renewed"
	second := httptest.NewRequest(http.MethodPost, "https://gc.test/satellites/sync", nil)
	creds.Authorize(second)

	require.Equal(t, "Bearer hss_first", first.Header.Get("Authorization"))
	require.Equal(t, "Bearer hss_renewed", second.Header.Get("Authorization"))
}

func TestClient_SyncDecodesSessionAndInventoryRequested(t *testing.T) {
	pending.Plan(t, "4.1", "Ground Control issues no sessions; the satellite authenticates every beat with its robot")
	expires := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	session := map[string]any{"token": "hss_renewed", "expires_at": expires.Format(time.RFC3339)}

	t.Run("sync", func(t *testing.T) {
		gc := newFakeGC(t, http.StatusOK, map[string]any{"events": []string{}, "session": session, "inventory_requested": true})
		c := newClient(t, gc.server.URL)

		res, err := c.Sync(context.Background(), gcclient.StatusReport{Name: "edge-01"})

		require.NoError(t, err)
		require.True(t, res.InventoryRequested)
		require.NotNil(t, res.Session)
		require.Equal(t, "hss_renewed", res.Session.Token)
		require.True(t, expires.Equal(res.Session.ExpiresAt))
	})

	t.Run("sync without a session", func(t *testing.T) {
		gc := newFakeGC(t, http.StatusOK, map[string]any{"events": []string{}})
		c := newClient(t, gc.server.URL)

		res, err := c.Sync(context.Background(), gcclient.StatusReport{Name: "edge-01"})

		require.NoError(t, err)
		require.Nil(t, res.Session, "the current session stays")
		require.False(t, res.InventoryRequested)
	})

	t.Run("token ZTR", func(t *testing.T) {
		body := map[string]any{"session": session}
		for k, v := range anyGCResponse {
			body[k] = v
		}
		gc := newFakeGC(t, http.StatusOK, body)
		c := newClient(t, gc.server.URL)

		res, err := c.ZeroTouch(context.Background(), "ztr-token")

		require.NoError(t, err)
		require.NotNil(t, res.Session)
		require.Equal(t, "hss_renewed", res.Session.Token)
	})
}
