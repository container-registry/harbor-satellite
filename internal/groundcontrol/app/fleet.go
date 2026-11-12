// Package app is Ground Control's composition root: it builds the
// adapters, hands them to the fleet services and the async plane, and
// gives the server what it needs as options, so the server never imports
// the async plane. `serve` and `worker` use it once the handlers call the
// services (plan item 1.4); until then nothing does.
package app

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/casbin"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/harbor2"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/satconfig"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/secrets"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
)

// The composition root asserts what the server and the worker receive, so
// a change of either side fails to build here.
var (
	_ server.SatelliteAuthenticator = (*authn.Sessions)(nil)
	_ async.Job                     = (*heartbeat.Flusher)(nil)
	_ async.Job                     = (*casbin.Policy)(nil)
	_ fleet.ConfigValidator         = satconfig.Validator{}
	_ fleet.Sealer                  = (*secrets.AESGCM)(nil)
	_ server.OperatorLogin          = operatorLogin{}
)

type FleetDeps struct {
	// Pools: admin writes run on Pools.API, so a busy fleet plane
	// cannot starve them.
	Pools  *dbpool.Pools
	Harbor env.Harbor
	// Server carries the session, heartbeat and stale settings of plan
	// item 4.
	Server env.Server
	// Webhook enables the webhook API (plan item 9.1).
	Webhook env.Webhook
	// SPIRE is nil when no SPIRE server is configured; SPIFFE
	// registration then fails.
	SPIRE *spiffe.ServerClient
	// Policy selects the /api route check (POLICY_ENGINE).
	Policy env.Policy
}

// Fleet is what a serve process runs: the services behind the handlers,
// the registry probe, which runs with or without a worker, and the
// satellite hot path of plan item 4: the sessions, this replica's
// heartbeat buffer and its flusher, which also runs whatever --worker is.
type Fleet struct {
	Services        *fleet.Services
	Probe           *async.RegistryProbe
	Sessions        *authn.Sessions
	HeartbeatBuffer *heartbeat.Buffer
	Flusher         *heartbeat.Flusher
	// Policy is the Casbin policy, nil with POLICY_ENGINE=builtin; its
	// poller is a job of every serve process.
	Policy *casbin.Policy
	// Rotations serves pending robot rotations to the heartbeat (plan item
	// 8.4) and Login is the OIDC operator login (8.1); both nil while
	// disabled.
	Rotations *async.RotationCache
	Login     *authn.OIDCLogin
}

// operatorLogin adapts authn.OIDCLogin to server.OperatorLogin.
type operatorLogin struct{ *authn.OIDCLogin }

func (l operatorLogin) Start(ctx context.Context) (redirectURL, binding string, err error) {
	start, err := l.OIDCLogin.Start(ctx)
	return start.RedirectURL, start.Binding, err
}

// NewFleet builds the services over postgres.Repository, a harbor2.Registry
// configured from deps.Harbor and, with SPIRE, a spire.WorkloadIdentity,
// with Settings(deps.Harbor, deps.Server, deps.Webhook). The probe pings the same
// registry without the rate limit every HARBOR_PROBE_INTERVAL and counts
// it stale after three intervals. Sessions run on a postgres.SessionStore
// and the flusher on a postgres.HeartbeatStore, both on Pools.Fleet, which
// is also the services' SatelliteRepository. The services validate configs
// with satconfig.Validator. With POLICY_ENGINE=casbin, casbin.New loads
// the repository's policies and the services and the server share it.
//
// The plan item 7, 8.1 and 8.4 parts (policy, rotations, login) stay nil
// until those items land.
func NewFleet(deps FleetDeps) (*Fleet, error) {
	if deps.Pools == nil || deps.Pools.API == nil {
		return nil, fmt.Errorf("%w: the fleet needs database pools", fleet.ErrInvalid)
	}
	registry, err := newRegistry(deps.Harbor)
	if err != nil {
		return nil, err
	}
	repo, err := postgres.NewRepository(deps.Pools.API)
	if err != nil {
		return nil, err
	}
	fleetRepo, err := postgres.NewRepository(fleetPool(deps.Pools))
	if err != nil {
		return nil, err
	}
	var identity fleet.WorkloadIdentity
	if deps.SPIRE != nil {
		if identity, err = spire.New(deps.SPIRE); err != nil {
			return nil, err
		}
	}
	hot := withHotPathDefaults(deps.Server.FleetHotPath)
	sessionStore, err := postgres.NewSessionStore(fleetPool(deps.Pools))
	if err != nil {
		return nil, err
	}
	sessions, err := authn.NewSessions(sessionStore, fleet.SystemClock{}, authn.SessionConfig{
		TTL: hot.SatelliteSessionTTL, CacheTTL: hot.SatelliteSessionCacheTTL, CacheSize: hot.SatelliteSessionCacheSize,
	})
	if err != nil {
		return nil, fmt.Errorf("satellite sessions: %w", err)
	}
	beatCfg := HeartbeatConfig(hot)
	buffer, err := heartbeat.NewBuffer(beatCfg)
	if err != nil {
		return nil, err
	}
	beatStore, err := postgres.NewHeartbeatStore(fleetPool(deps.Pools), fleet.SystemClock{})
	if err != nil {
		return nil, err
	}
	flusher, err := heartbeat.NewFlusher(buffer, beatStore, beatCfg)
	if err != nil {
		return nil, err
	}
	services, err := fleet.NewServices(fleet.Deps{
		Repository:          repo,
		SatelliteRepository: fleetRepo,
		Registry:            registry,
		Identity:            identity,
		Settings:            Settings(deps.Harbor, deps.Server, deps.Webhook),
		Sessions:            sessions,
		HeartbeatSink:       buffer,
	})
	if err != nil {
		return nil, err
	}
	interval := deps.Harbor.ProbeInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	probe, err := async.NewRegistryProbe(registry, interval, 3*interval)
	if err != nil {
		return nil, err
	}
	return &Fleet{Services: services, Probe: probe, Sessions: sessions, HeartbeatBuffer: buffer, Flusher: flusher}, nil
}

// withHotPathDefaults fills the plan item 4 settings a caller left zero
// with the env defaults, so a composition without the environment works.
func withHotPathDefaults(h env.FleetHotPath) env.FleetHotPath {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	defInt := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&h.SatelliteSessionTTL, 720*time.Hour)
	def(&h.SatelliteSessionCacheTTL, 30*time.Second)
	defInt(&h.SatelliteSessionCacheSize, 10000)
	def(&h.HeartbeatFlushInterval, 5*time.Second)
	defInt(&h.HeartbeatBufferMax, 10000)
	defInt(&h.HeartbeatMaxFlushFailures, 3)
	def(&h.StaleCheckInterval, time.Minute)
	def(&h.SessionExpiryInterval, time.Hour)
	defInt(&h.StatusRetentionDays, 7)
	if h.StatusPartitionsAhead < 0 {
		h.StatusPartitionsAhead = 3
	}
	return h
}

// HeartbeatConfig maps HEARTBEAT_* to the buffer and flusher settings.
func HeartbeatConfig(h env.FleetHotPath) heartbeat.Config {
	return heartbeat.Config{
		FlushInterval:     h.HeartbeatFlushInterval,
		MaxPending:        h.HeartbeatBufferMax,
		MaxFlushFailures:  h.HeartbeatMaxFlushFailures,
		FinalFlushTimeout: 10 * time.Second,
	}
}

func fleetPool(p *dbpool.Pools) *sql.DB {
	if p.Fleet != nil {
		return p.Fleet
	}
	return p.API
}

func asyncPool(p *dbpool.Pools) *sql.DB {
	if p.Async != nil {
		return p.Async
	}
	return p.API
}

func newRegistry(h env.Harbor) (*harbor2.Registry, error) {
	return harbor2.New(harbor2.Config{URL: h.URL, Username: h.Username, Password: h.Password, Insecure: strings.HasPrefix(h.URL, "http://")})
}

// outboxReader adapts the outbox store to server.OutboxReader.
type outboxReader struct {
	store      *postgres.OutboxStore
	maxAttempt int
}

func (o outboxReader) ListOutbox(ctx context.Context, failedOnly bool, limit int) ([]server.OutboxEventResponse, error) {
	minAttempts := 0
	if failedOnly {
		minAttempts = o.maxAttempt
	}
	events, err := o.store.Stuck(ctx, minAttempts, limit)
	if err != nil {
		return nil, err
	}
	out := make([]server.OutboxEventResponse, 0, len(events))
	for _, e := range events {
		out = append(out, server.OutboxEventResponse{
			ID: e.ID, Kind: string(e.Kind), AggregateType: string(e.Aggregate.Type), AggregateID: e.Aggregate.ID,
			Attempts: e.Attempts, CreatedAt: e.CreatedAt, NextAttemptAt: e.NextAttemptAt, LastError: e.LastError,
		})
	}
	return out, nil
}

// ServerOptions hands the server the services (server.WithFleet), the
// probe (server.WithHarborContact), the outbox reader for
// /api/ops/outbox (server.WithOutboxReader), the sessions
// (server.WithSatelliteAuthenticator(f.Sessions)), when set, the policy
// (server.WithPolicy(f.Policy)) and, with OIDC, the operator login
// (server.WithOperatorLogin).
func (f *Fleet) ServerOptions(pools *dbpool.Pools) []server.Option {
	opts := []server.Option{server.WithFleet(f.Services)}
	if f.Probe != nil {
		opts = append(opts, server.WithHarborContact(f.Probe, f.Probe.StaleAfter()))
	}
	if pools != nil {
		if store, err := postgres.NewOutboxStore(asyncPool(pools)); err == nil {
			opts = append(opts, server.WithOutboxReader(outboxReader{store: store, maxAttempt: env.GC.Outbox.MaxAttempts}))
		}
	}
	if f.Sessions != nil {
		opts = append(opts, server.WithSatelliteAuthenticator(f.Sessions))
	}
	if f.Policy != nil {
		opts = append(opts, server.WithPolicy(f.Policy))
	}
	if f.Login != nil {
		opts = append(opts, server.WithOperatorLogin(operatorLogin{f.Login}))
	}
	return opts
}

// Settings maps the Harbor, server and webhook environment to the fleet
// settings; every composition uses it, so the services and the worker agree.
func Settings(h env.Harbor, srv env.Server, webhooks env.Webhook) fleet.Settings {
	return fleet.Settings{
		RegistryURL:            h.URL,
		RobotDuration:          time.Duration(h.RobotDurationDaysValue()) * 24 * time.Hour,
		TokenTTL:               fleet.DefaultTokenTTL,
		StaleThreshold:         cmp.Or(srv.StaleThreshold, 15*time.Minute),
		HeartbeatFlushInterval: srv.HeartbeatFlushInterval,
		WebhooksEnabled:        webhooks.Enabled,
	}
}
