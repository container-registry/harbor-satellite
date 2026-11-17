package budget

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestAdmitSkipsWhenTheWindowIsFull(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{Quota: Quota{Enabled: true, LimitBytes: 100, Window: WindowDay}})

	require.NoError(t, account.Admit(40))
	account.record(40)
	require.NoError(t, account.Admit(60))
	require.ErrorIs(t, account.Admit(61), ErrQuotaExceeded)
}

func TestDisabledPolicyStillReportsThroughput(t *testing.T) {
	account := New()
	body := account.Wrap(io.NopCloser(bytes.NewReader([]byte("hello"))))
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	require.NoError(t, body.Close())
	stats := account.Snapshot()
	require.Equal(t, int64(5), stats.BytesTotal)
	require.Equal(t, int64(0), stats.BytesInWindow)
	require.Equal(t, float64(5), stats.BytesPerSecond)
	require.NoError(t, account.Admit(1<<30))
}

func TestSyncWindowResetsAndDayWindowDoesNot(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	account := New()
	account.now = func() time.Time { return now }
	account.SetPolicy(Policy{Quota: Quota{Enabled: true, LimitBytes: 100, Window: WindowSync}})
	account.record(80)
	require.ErrorIs(t, account.Admit(30), ErrQuotaExceeded)
	account.BeginSync()
	require.NoError(t, account.Admit(30))

	account.SetPolicy(Policy{Quota: Quota{Enabled: true, LimitBytes: 100, Window: WindowDay}})
	account.record(90)
	account.BeginSync()
	require.ErrorIs(t, account.Admit(20), ErrQuotaExceeded)

	now = now.Add(24 * time.Hour)
	require.NoError(t, account.Admit(20))
	require.Equal(t, int64(0), account.Snapshot().BytesInWindow)
}

func TestWeekWindowRollsOnTheUTCWeek(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) // Monday
	account := New()
	account.now = func() time.Time { return now }
	account.SetPolicy(Policy{Quota: Quota{Enabled: true, LimitBytes: 10, Window: WindowWeek}})
	account.record(10)
	require.ErrorIs(t, account.Admit(1), ErrQuotaExceeded)

	now = now.Add(6 * 24 * time.Hour)
	require.ErrorIs(t, account.Admit(1), ErrQuotaExceeded)

	now = now.Add(24 * time.Hour)
	require.NoError(t, account.Admit(1))
}

func TestWrapCountsBytesAndStopsWhenCanceled(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{
		Speed: Speed{Enabled: true, BytesPerSecond: 1},
		Quota: Quota{Enabled: true, LimitBytes: 100, Window: WindowSync},
	})
	payload := bytes.Repeat([]byte("x"), 8)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	body := account.WrapContext(ctx, io.NopCloser(bytes.NewReader(payload)))

	buf := make([]byte, 4)
	n, err := body.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	_, err = body.Read(buf)
	require.ErrorContains(t, err, "context deadline")
	require.NoError(t, body.Close())
	require.Equal(t, int64(1), account.Snapshot().BytesTotal)
	require.Equal(t, int64(1), account.Snapshot().BytesInWindow)
}

func TestPolicyFromDiagramQuantities(t *testing.T) {
	policy, err := PolicyFrom(config.NetworkConfig{
		Speed: config.NetworkSpeedConfig{Enabled: true, Limit: "10Mbps"},
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "5GB", Window: "day"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1_250_000), policy.Speed.BytesPerSecond)
	require.Equal(t, int64(5_000_000_000), policy.Quota.LimitBytes)
	require.Equal(t, WindowDay, policy.Quota.Window)
}

func TestContextTransportMetersOnlyMarkedRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("abcdef")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	require.NoError(t, Apply(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "1GB", Window: "day"},
	}))
	t.Cleanup(func() {
		require.NoError(t, Apply(config.NetworkConfig{}))
	})

	client := &http.Client{Transport: ContextTransport(nil)}
	before := Ensure().Snapshot().BytesTotal
	plain, err := client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.ReadAll(plain.Body)
	require.NoError(t, err)
	require.NoError(t, plain.Body.Close())
	require.Equal(t, before, Ensure().Snapshot().BytesTotal)

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	request = request.WithContext(WithMeter(request.Context()))
	marked, err := client.Do(request)
	require.NoError(t, err)
	_, err = io.ReadAll(marked.Body)
	require.NoError(t, err)
	require.NoError(t, marked.Body.Close())
	require.Equal(t, before+6, Ensure().Snapshot().BytesTotal)
}

func TestSetPolicyUpdatesTheLiveLimiter(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{Speed: Speed{Enabled: true, BytesPerSecond: 1}})
	account.SetPolicy(Policy{Speed: Speed{Enabled: true, BytesPerSecond: 2_000}})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	body := account.WrapContext(ctx, io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 32))))
	got := make([]byte, 32)
	n, err := io.ReadFull(body, got)
	require.NoError(t, err)
	require.Equal(t, 32, n)
	require.NoError(t, body.Close())
	require.Equal(t, int64(32), account.Snapshot().BytesTotal)
}
