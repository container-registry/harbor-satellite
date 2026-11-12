package state

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/budget"
	"github.com/stretchr/testify/require"
)

func TestRecordDownloadStatsCopiesTheSnapshot(t *testing.T) {
	account := budget.Ensure()
	before := account.Snapshot()
	account.BeginSync()

	req := &StatusReportParams{}
	recordDownloadStats(req)

	require.Equal(t, before.BytesPerSecond, req.DownloadBytesPerSecond)
	require.Equal(t, int64(0), req.BytesInWindow)
	require.Equal(t, before.BytesTotal, req.BytesTotal)
}
