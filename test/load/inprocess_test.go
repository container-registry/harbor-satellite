//go:build load

package load

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

const inProcessAdminPassword = "LoadTest-Admin-Pass1"

// inProcessGC composes Ground Control as serve --worker=embedded does, on
// the database of TEST_DATABASE_URL and a Harbor stub, and returns its URL
// and admin password. It skips without TEST_DATABASE_URL.
func inProcessGC(t *testing.T) (string, string) {
	t.Helper()
	db := dbtest.New(t)
	stub := harbortest.New(t)
	for k, v := range map[string]string{
		"DB_HOST": db.Host, "DB_PORT": db.Port, "DB_USERNAME": db.User, "DB_PASSWORD": db.Password, "DB_DATABASE": db.Name,
		"DB_SSLMODE": db.SSLMode, "HARBOR_URL": stub.URL(), "HARBOR_USERNAME": harbortest.Username, "HARBOR_PASSWORD": harbortest.Password,
		"ADMIN_PASSWORD": inProcessAdminPassword, "SPIFFE_SERVER_ENABLED": "false", "EMBEDDED_SPIRE_ENABLED": "false",
		"SPIRE_SERVER_SOCKET": "", "AUDIT_LOG_ENABLED": "false", "TLS_CERT_FILE": "", "TLS_KEY_FILE": "",
		// Onboarding 5000 satellites from one test IP would exhaust the
		// per-IP ZTR budget; the fleet limit under test stays the default.
		"RATE_LIMIT_SPLIT": "true", "RATE_LIMIT_ANONYMOUS": "1000000/1m", "DB_API_MAX_OPEN_CONNS": "40",
	} {
		t.Setenv(k, v)
	}
	previous := env.GC
	require.NoError(t, env.LoadGC())
	t.Cleanup(func() { env.GC = previous })

	ctx, cancel := context.WithCancel(context.Background())
	var fleet *app.Fleet
	var pools *dbpool.Pools
	res, err := server.NewServer(ctx, server.WithFleetFactory(func(_ context.Context, p *dbpool.Pools, spire *spiffe.ServerClient) ([]server.Option, error) {
		f, err := app.NewFleet(app.FleetDeps{Pools: p, Harbor: env.GC.Harbor, Server: env.GC.Server, Webhook: env.GC.Webhook, SPIRE: spire, Policy: env.GC.Policy})
		if err != nil {
			return nil, err
		}
		fleet, pools = f, p
		return f.ServerOptions(p), nil
	}))
	require.NoError(t, err)
	worker, closeWorker, err := app.NewWorker(ctx, app.WorkerDeps{
		Pools: pools, Harbor: env.GC.Harbor, Server: env.GC.Server, Config: app.PublisherConfig(env.GC.Outbox), Webhook: env.GC.Webhook,
	})
	require.NoError(t, err)
	done := make(chan struct{}, 3)
	for _, run := range []func(context.Context) error{worker.Run, fleet.Flusher.Run, fleet.Probe.Run} {
		go func() {
			if err := run(ctx); err != nil {
				t.Errorf("Ground Control job: %v", err)
			}
			done <- struct{}{}
		}()
	}
	ts := httptest.NewServer(res.Server.Handler)
	t.Cleanup(func() {
		ts.Close()
		cancel()
		for range 3 {
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Error("a Ground Control job did not stop")
			}
		}
		require.NoError(t, closeWorker())
		require.NoError(t, res.Close())
	})
	return ts.URL, inProcessAdminPassword
}
