package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/state"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const gcAdminPassword = "Contract-Admin-1"

// groundControl is Ground Control composed as `serve --worker=embedded`
// composes it, on a fresh database, with Harbor replaced by a stub.
type groundControl struct {
	url   string
	db    *dbtest.DB
	admin string
}

func startGroundControl(t *testing.T) *groundControl {
	t.Helper()
	if !dbtest.Enabled() {
		t.Skipf("set %s to run Ground Control against Postgres", dbtest.EnvURL)
	}
	db := dbtest.New(t)
	harbor := harbortest.New(t)
	for k, v := range map[string]string{
		"DB_HOST": db.Host, "DB_PORT": db.Port, "DB_USERNAME": db.User, "DB_PASSWORD": db.Password,
		"DB_DATABASE": db.Name, "DB_SSLMODE": db.SSLMode,
		"HARBOR_URL": harbor.URL(), "HARBOR_USERNAME": harbortest.Username, "HARBOR_PASSWORD": harbortest.Password,
		"SKIP_HARBOR_HEALTH_CHECK": "false", "ADMIN_PASSWORD": gcAdminPassword,
		"SPIFFE_SERVER_ENABLED": "false", "EMBEDDED_SPIRE_ENABLED": "false", "SPIRE_SERVER_SOCKET": "",
		"AUDIT_LOG_ENABLED": "false", "TLS_CERT_FILE": "", "TLS_KEY_FILE": "",
		"HEARTBEAT_FLUSH_INTERVAL": "100ms",
		// One limiter of 10 per minute would cover login, ZTR and sync.
		"RATE_LIMIT_SPLIT": "true", "RATE_LIMIT_ANONYMOUS": "100/1m", "RATE_LIMIT_FLEET": "100/1s",
	} {
		t.Setenv(k, v)
	}
	previous := env.GC
	require.NoError(t, env.LoadGC())
	t.Cleanup(func() { env.GC = previous })

	ctx, cancel := context.WithCancel(context.Background())
	var composed *app.Fleet
	var pools *dbpool.Pools
	factory := func(_ context.Context, p *dbpool.Pools, spire *spiffe.ServerClient) ([]server.Option, error) {
		f, err := app.NewFleet(app.FleetDeps{
			Pools: p, Harbor: env.GC.Harbor, Server: env.GC.Server, Webhook: env.GC.Webhook, SPIRE: spire, Policy: env.GC.Policy,
			RobotRotation: env.GC.RobotRotation, DataEncryptionKey: env.GC.DataEncryptionKey, OIDC: env.GC.OIDC,
		})
		if err != nil {
			return nil, err
		}
		composed, pools = f, p
		return f.ServerOptions(p), nil
	}
	res, err := server.NewServer(ctx, server.WithFleetFactory(factory))
	require.NoError(t, err)
	worker, closeWorker, err := app.NewWorker(ctx, app.WorkerDeps{
		Pools: pools, Harbor: env.GC.Harbor, Server: env.GC.Server, Config: app.PublisherConfig(env.GC.Outbox),
		Webhook: env.GC.Webhook, OutboxRetention: env.GC.Outbox.Retention,
	})
	require.NoError(t, err)
	done := make(chan struct{}, 2)
	go func() { _ = worker.Run(ctx); done <- struct{}{} }()           //nolint:errcheck // It stops with ctx.
	go func() { _ = composed.Flusher.Run(ctx); done <- struct{}{} }() //nolint:errcheck // It stops with ctx.
	ts := httptest.NewServer(res.Server.Handler)
	t.Cleanup(func() {
		ts.Close()
		cancel()
		<-done
		<-done
		require.NoError(t, closeWorker())
		require.NoError(t, res.Close())
	})

	gc := &groundControl{url: ts.URL, db: db}
	var login server.LoginResponse
	gc.post(t, "/login", server.LoginRequest{Username: "admin", Password: gcAdminPassword}, http.StatusOK, &login)
	gc.admin = login.Token
	return gc
}

// post sends body as JSON with the admin session, once there is one.
func (gc *groundControl) post(t *testing.T, path string, body any, want int, out any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, gc.url+path, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if gc.admin != "" {
		req.Header.Set("Authorization", "Bearer "+gc.admin)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, want, resp.StatusCode, "POST %s", path)
	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
}

// registerSatellite registers name in a config and a group and returns
// its ZTR token.
func (gc *groundControl) registerSatellite(t *testing.T, name string) string {
	t.Helper()
	gc.post(t, "/api/configs", map[string]any{"config_name": "edge-config", "config": map[string]any{
		"app_config": map[string]any{
			"log_level": "info", "state_replication_interval": "@every 00h00m30s",
			"register_satellite_interval": "@every 00h00m05s", "heartbeat_interval": "@every 00h00m30s",
		},
	}}, http.StatusCreated, nil)
	gc.post(t, "/api/groups/sync", server.GroupSyncRequest{Group: "edge-group", Artifacts: []server.GroupArtifact{{
		Repository: "library/alpine", Tag: []string{"latest"}, Type: "image",
		Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000001",
	}}}, http.StatusOK, nil)
	var tok server.TokenSatelliteRegistrationResponse
	gc.post(t, "/api/satellites", server.TokenSatelliteRegistrationRequest{
		Name: name, ConfigName: "edge-config", Groups: []string{"edge-group"},
	}, http.StatusOK, &tok)
	require.NotEmpty(t, tok.Token)
	return tok.Token
}

// TestSatelliteJournalReachesGroundControl runs the satellite's own ZTR and
// heartbeat code with a journal against a composed Ground Control: every
// beat answers 200 with an ack, and the records land in the outbox once.
func TestSatelliteJournalReachesGroundControl(t *testing.T) {
	gc := startGroundControl(t)
	token := gc.registerSatellite(t, "edge-01")
	ctx := testContext()

	dir := t.TempDir()
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), token, gc.url, false, &config.Config{
		AppConfig: config.AppConfig{GroundControlURL: config.URL(gc.url), HeartbeatInterval: "@every 30s", UseUnsecure: true},
	})
	require.NoError(t, err)
	// ZTR answers 503 until the worker has created the satellite's robot.
	require.Eventually(t, func() bool { return state.NewZtrProcess(cm).Execute(ctx) == nil }, 30*time.Second, 200*time.Millisecond)
	require.NotEmpty(t, cm.GetStateURL())

	j, err := journal.Open(journal.Options{Dir: filepath.Join(dir, "journal")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, j.Close()) })
	const records = 5
	for i := range records {
		payload, marshalErr := json.Marshal(edgejournal.Replication{
			SchemaVersion: edgejournal.SchemaVersion, CycleID: "c-1", Action: edgejournal.ActionReplicated, Group: "edge-group",
			Repository: "library/alpine", Tag: fmt.Sprintf("3.%d", i), Digest: "sha256:aaa", Bytes: 1024,
		})
		require.NoError(t, marshalErr)
		_, marshalErr = j.Append(ctx, journal.Record{Type: edgejournal.RecordReplication, RecordedAt: time.Now().UTC(), Payload: payload})
		require.NoError(t, marshalErr)
	}
	log := zerolog.Nop()
	reporter := state.NewStatusReportingProcess(cm, events.NewEventScheduler(&log))
	reporter.SetJournal(j)

	// The first beat carries one record and the robot's Basic auth, and
	// gets a session; the second carries the rest over the session.
	for beat := 1; beat <= 2; beat++ {
		require.NoError(t, reporter.Execute(ctx), "beat %d answers 200", beat)
	}
	require.Equal(t, int64(records), j.Acked(), "Ground Control acknowledged every record")
	require.NotEmpty(t, cm.GetGCSession().Token, "the journaled beat still delivered the session")
	require.NoError(t, reporter.Execute(ctx), "a beat with nothing to upload")

	rows, err := gc.db.QueryContext(context.Background(), `
		SELECT o.kind, (o.payload->>'seq')::BIGINT, o.payload->>'name', o.payload->>'tag'
		FROM event_outbox o JOIN satellites s ON s.id = o.aggregate_id
		WHERE o.aggregate_type = 'edge' AND s.name = 'edge-01' AND o.done_at IS NOT NULL
		ORDER BY o.id`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var seqs []int64
	for rows.Next() {
		var kind, name, tag string
		var seq int64
		require.NoError(t, rows.Scan(&kind, &seq, &name, &tag))
		require.Equal(t, "EdgeReplicated", kind)
		require.Equal(t, "edge-01", name)
		require.Equal(t, fmt.Sprintf("3.%d", seq-1), tag)
		seqs = append(seqs, seq)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{1, 2, 3, 4, 5}, seqs, "each record once, in order")

	require.Eventually(t, func() bool {
		var seen *time.Time
		err := gc.db.QueryRowContext(context.Background(), "SELECT last_seen FROM satellites WHERE name = 'edge-01'").Scan(&seen)
		return err == nil && seen != nil
	}, 10*time.Second, 100*time.Millisecond, "the flusher recorded the heartbeats")
}
