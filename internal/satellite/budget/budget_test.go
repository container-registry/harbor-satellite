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

func TestDisabledQuotaLeavesTheWindowAtZero(t *testing.T) {
	account := New()
	body := account.Wrap(io.NopCloser(bytes.NewReader([]byte("hello"))))
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	require.NoError(t, body.Close())
	stats := account.Snapshot()
	require.Equal(t, int64(5), stats.BytesTotal)
	require.Equal(t, int64(0), stats.BytesInWindow)
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

func TestWrapCountsBytesInTheOpenWindow(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{Quota: Quota{Enabled: true, LimitBytes: 100, Window: WindowSync}})
	body := account.Wrap(io.NopCloser(bytes.NewReader([]byte("abcdef"))))
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "abcdef", string(got))
	require.NoError(t, body.Close())
	stats := account.Snapshot()
	require.Equal(t, int64(6), stats.BytesTotal)
	require.Equal(t, int64(6), stats.BytesInWindow)
}

func TestPolicyFromQuotaQuantity(t *testing.T) {
	policy, err := PolicyFrom(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "5GB", Window: "day"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(5_000_000_000), policy.Quota.LimitBytes)
	require.Equal(t, WindowDay, policy.Quota.Window)

	disabled, err := PolicyFrom(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: false, Limit: "lots", Window: "month"},
	})
	require.NoError(t, err)
	require.False(t, disabled.Quota.Enabled)
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
	require.Equal(t, int64(6), Ensure().Snapshot().BytesInWindow)
}

func TestContextTransportUsesTheRequestContext(t *testing.T) {
	account := New()
	body := account.WrapContext(context.Background(), io.NopCloser(bytes.NewReader([]byte("xy"))))
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "xy", string(got))
	require.NoError(t, body.Close())
	require.Equal(t, int64(2), account.Snapshot().BytesTotal)
}
