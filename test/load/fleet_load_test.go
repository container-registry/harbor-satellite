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
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
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

func newGC(t *testing.T) *gc {
	t.Helper()
	url, password := os.Getenv("LOAD_GC_URL"), os.Getenv("LOAD_ADMIN_PASSWORD")
	if url == "" || password == "" {
		t.Skip("LOAD_GC_URL and LOAD_ADMIN_PASSWORD point the load test at a running Ground Control")
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
	require.Equal(g.t, want, resp.StatusCode, "%s %s: %s", method, path, raw)
	if out != nil {
		require.NoError(g.t, json.Unmarshal(raw, out), string(raw))
	}
	return elapsed
}

func p99(durations []time.Duration) time.Duration {
	sorted := slices.Clone(durations)
	slices.Sort(sorted)
	return sorted[len(sorted)*99/100]
}

// TestFleetLoad5000 onboards 5000 satellites through session ZTR and lets
// every one beat three rounds: no beat may get 429, and the p99 of the last
// round may not exceed twice the first's (plan items 4.1, 4.2, 4.6).
func TestFleetLoad5000(t *testing.T) {
	pending.Plan(t, "4.2", "satellites have no sessions; ZTR returns none")
	g := newGC(t)
	prefix := fmt.Sprintf("load-%d", time.Now().Unix())
	g.do(http.MethodPost, "/api/configs", map[string]any{"config_name": prefix, "config": map[string]any{"app_config": map[string]any{"heartbeat_interval": "@every 00h00m30s"}}}, g.admin, http.StatusCreated, nil)

	tokens := make([]string, satellites)
	for i := range tokens {
		name := fmt.Sprintf("%s-%04d", prefix, i)
		var reg struct {
			Token string `json:"token"`
		}
		g.do(http.MethodPost, "/api/satellites", map[string]any{"name": name, "config_name": prefix}, g.admin, http.StatusOK, &reg)
		var ztr struct {
			Session *struct {
				Token string `json:"token"`
			} `json:"session"`
		}
		g.do(http.MethodPost, "/satellites/ztr", map[string]string{"token": reg.Token}, "", http.StatusOK, &ztr)
		require.NotNil(t, ztr.Session, "%s: ZTR issues a session", name)
		tokens[i] = ztr.Session.Token
	}

	var rounds [][]time.Duration
	for range 3 {
		latencies := make([]time.Duration, satellites)
		var wg sync.WaitGroup
		for i, token := range tokens {
			wg.Go(func() {
				name := fmt.Sprintf("%s-%04d", prefix, i)
				latencies[i] = g.do(http.MethodPost, "/satellites/sync", map[string]any{
					"name": name, "activity": "idle", "state_report_interval": "@every 00h00m30s", "request_created_time": time.Now().UTC(),
				}, token, http.StatusOK, nil)
			})
		}
		wg.Wait()
		rounds = append(rounds, latencies)
	}

	first, last := p99(rounds[0]), p99(rounds[len(rounds)-1])
	t.Logf("p99 first round %s, last round %s", first, last)
	require.LessOrEqual(t, last, 2*first, "p99 stays flat")
}
