//go:build load

package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const satellites = 5000

// gc is a Ground Control at LOAD_GC_URL, administered with
// LOAD_ADMIN_PASSWORD.
type gc struct {
	t     *testing.T
	url   string
	admin string
}

// newGC is the Ground Control at LOAD_GC_URL, or one composed in this
// process on TEST_DATABASE_URL and a Harbor stub.
func newGC(t *testing.T) *gc {
	t.Helper()
	url, password := os.Getenv("LOAD_GC_URL"), os.Getenv("LOAD_ADMIN_PASSWORD")
	if url == "" || password == "" {
		url, password = inProcessGC(t)
	}
	g := &gc{t: t, url: url}
	var login struct {
		Token string `json:"token"`
	}
	g.do(http.MethodPost, "/login", map[string]string{"username": "admin", "password": password}, "", http.StatusOK, &login)
	g.admin = login.Token
	return g
}

func (g *gc) do(method, path string, body any, bearer string, want int, out any) time.Duration {
	g.t.Helper()
	status, elapsed := g.try(method, path, body, bearer, out)
	require.Equal(g.t, want, status, "%s %s", method, path)
	return elapsed
}

// try sends one request and decodes a 2xx answer into out.
func (g *gc) try(method, path string, body any, bearer string, out any) (int, time.Duration) {
	g.t.Helper()
	data, err := json.Marshal(body)
	require.NoError(g.t, err)
	req, err := http.NewRequestWithContext(context.Background(), method, g.url+path, bytes.NewReader(data))
	require.NoError(g.t, err)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	require.NoError(g.t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(g.t, err)
	if out != nil && resp.StatusCode < 300 {
		require.NoError(g.t, json.Unmarshal(raw, out), string(raw))
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusServiceUnavailable {
		g.t.Logf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	return resp.StatusCode, elapsed
}

// ztr retries while the async plane has not created the robot (503).
func (g *gc) ztr(token string) string {
	g.t.Helper()
	var ztr struct {
		Session *struct {
			Token string `json:"token"`
		} `json:"session"`
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		status, _ := g.try(http.MethodPost, "/satellites/ztr", map[string]string{"token": token}, "", &ztr)
		if status == http.StatusOK {
			require.NotNil(g.t, ztr.Session, "ZTR issues a session")
			return ztr.Session.Token
		}
		require.Equal(g.t, http.StatusServiceUnavailable, status, "ZTR before the robot exists answers 503")
		require.True(g.t, time.Now().Before(deadline), "the robot was not provisioned in time")
		time.Sleep(200 * time.Millisecond)
	}
}

// parallel runs fn for every index with at most limit in flight.
func parallel(n, limit int, fn func(i int)) {
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range n {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(i)
		})
	}
	wg.Wait()
}

func p99(durations []time.Duration) time.Duration { return percentile(durations, 99) }

func percentile(durations []time.Duration, p int) time.Duration {
	sorted := slices.Clone(durations)
	slices.Sort(sorted)
	return sorted[len(sorted)*p/100]
}

// TestFleetLoad5000 onboards 5000 satellites through session ZTR and lets
// every one beat three rounds: no beat may get 429, and the p99 of the last
// round may not exceed twice the first's (plan items 4.1, 4.2, 4.6).
func TestFleetLoad5000(t *testing.T) {
	g := newGC(t)
	prefix := fmt.Sprintf("load-%d", time.Now().Unix())
	g.do(http.MethodPost, "/api/configs", map[string]any{"config_name": prefix, "config": map[string]any{"app_config": map[string]any{"heartbeat_interval": "@every 00h00m30s"}}}, g.admin, http.StatusCreated, nil)

	regTokens := make([]string, satellites)
	parallel(satellites, 32, func(i int) {
		var reg struct {
			Token string `json:"token"`
		}
		g.do(http.MethodPost, "/api/satellites", map[string]any{"name": fmt.Sprintf("%s-%04d", prefix, i), "config_name": prefix}, g.admin, http.StatusOK, &reg)
		regTokens[i] = reg.Token
	})
	tokens := make([]string, satellites)
	parallel(satellites, 32, func(i int) { tokens[i] = g.ztr(regTokens[i]) })

	var rounds [][]time.Duration
	var limited atomic.Int64
	for range 3 {
		latencies := make([]time.Duration, satellites)
		parallel(satellites, 256, func(i int) {
			name := fmt.Sprintf("%s-%04d", prefix, i)
			status, elapsed := g.try(http.MethodPost, "/satellites/sync", map[string]any{
				"name": name, "activity": "idle", "state_report_interval": "@every 00h00m30s", "request_created_time": time.Now().UTC(),
			}, tokens[i], nil)
			if status == http.StatusTooManyRequests {
				limited.Add(1)
			}
			require.Equal(t, http.StatusOK, status, name)
			latencies[i] = elapsed
		})
		rounds = append(rounds, latencies)
	}
	require.Zero(t, limited.Load(), "no beat is rate limited")

	first, last := p99(rounds[0]), p99(rounds[len(rounds)-1])
	t.Logf("%d satellites: p99 first round %s, last round %s, p50 last round %s", satellites, first, last, percentile(rounds[len(rounds)-1], 50))
	require.LessOrEqual(t, last, 2*first, "p99 stays flat")
}
