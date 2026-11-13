package state

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/budget"
	"github.com/stretchr/testify/require"
)

func TestRecordDownloadStatsCopiesTheSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("abcdef")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	account := budget.Ensure()
	account.BeginSync()
	client := &http.Client{Transport: budget.Transport(nil)}
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	req := &StatusReportParams{}
	recordDownloadStats(req)

	stats := account.Snapshot()
	require.Equal(t, stats.BytesPerSecond, req.DownloadBytesPerSecond)
	require.Equal(t, stats.BytesInWindow, req.BytesInWindow)
	require.Equal(t, stats.BytesTotal, req.BytesTotal)
	require.Equal(t, int64(6), req.BytesInWindow)
	require.GreaterOrEqual(t, req.BytesTotal, int64(6))
	require.GreaterOrEqual(t, req.DownloadBytesPerSecond, float64(6))
}
