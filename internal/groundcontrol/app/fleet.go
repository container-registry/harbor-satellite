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
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/oidc"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/satconfig"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/secrets"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/telemetry"
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
	// RobotRotation and DataEncryptionKey enable the rotation cache that
	// delivers successor robots with heartbeats (plan item 8.4).
	RobotRotation     env.RobotRotation
	DataEncryptionKey string
	// OIDC enables operator login through an OIDC provider (plan item 8.1).
	OIDC env.OIDC
	// OutboxMaxAttempts (OUTBOX_MAX_ATTEMPTS) separates pending from
	// failed outbox events in /metrics; 0 means the default 10.
	OutboxMaxAttempts int
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
	// Metrics is Ground Control's /metrics; it records requests and
	// heartbeats whether or not serve listens for scrapes.
	Metrics *telemetry.Server
}

// operatorLogin adapts authn.OIDCLogin to server.OperatorLogin.
type operatorLogin struct{ *authn.OIDCLogin }

func (l operatorLogin) Start(ctx context.Context) (redirectURL, binding string, expiresAt time.Time, err error) {
	start, err := l.OIDCLogin.Start(ctx)
	return start.RedirectURL, start.Binding, start.ExpiresAt, err
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
// With ROBOT_ROTATION_ENABLED the heartbeat delivers pending rotations
// from a RotationCache on Pools.Fleet; with OIDC_ENABLED the operator
// login discovers the provider at start-up.
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
	metrics, err := NewMetrics(deps.Pools, buffer, cmp.Or(deps.OutboxMaxAttempts, 10))
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	flusher, err := heartbeat.NewFlusher(buffer, timedBeatStore{Store: beatStore, rec: metrics.Recorder()}, beatCfg)
	if err != nil {
		return nil, err
	}
	var policy *casbin.Policy
	var servicePolicy fleet.Policy
	if deps.Policy.Engine == env.PolicyEngineCasbin {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		poll := deps.Policy.PollInterval
		if poll <= 0 {
			poll = 10 * time.Second
		}
		if policy, err = casbin.New(ctx, repo.Autocommit().Policies(), poll); err != nil {
			return nil, fmt.Errorf("load the casbin policy: %w", err)
		}
		servicePolicy = policy
	}
	var rotations *async.RotationCache
	var rotationSource fleet.RotationSource
	if deps.RobotRotation.Enabled {
		sealer, rotationErr := NewSealer(deps.DataEncryptionKey)
		if rotationErr != nil {
			return nil, fmt.Errorf("ROBOT_ROTATION_ENABLED: %w", rotationErr)
		}
		store, rotationErr := postgres.NewRotationStore(fleetPool(deps.Pools))
		if rotationErr != nil {
			return nil, rotationErr
		}
		if rotations, rotationErr = async.NewRotationCache(store, sealer, cmp.Or(deps.RobotRotation.CacheRefresh, 30*time.Second)); rotationErr != nil {
			return nil, rotationErr
		}
		rotationSource = rotations
	}
	var webhooks fleet.WebhookRepo
	var webhookSealer fleet.Sealer
	if deps.Webhook.Enabled {
		if webhookSealer, err = NewSealer(deps.DataEncryptionKey); err != nil {
			return nil, fmt.Errorf("WEBHOOKS_ENABLED: %w", err)
		}
		if webhooks, err = postgres.NewWebhookRepo(deps.Pools.API); err != nil {
			return nil, err
		}
	}
	services, err := fleet.NewServices(fleet.Deps{
		Webhooks:            webhooks,
		Sealer:              webhookSealer,
		Policy:              servicePolicy,
		Rotations:           rotationSource,
		Repository:          repo,
		SatelliteRepository: fleetRepo,
		Registry:            registry,
		Identity:            identity,
		Settings:            Settings(deps.Harbor, deps.Server, deps.Webhook),
		Sessions:            sessions,
		HeartbeatSink:       countingSink{HeartbeatSink: buffer, rec: metrics.Recorder()},
		ConfigValidator:     satconfig.Validator{},
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
	f := &Fleet{Services: services, Probe: probe, Sessions: sessions, HeartbeatBuffer: buffer, Flusher: flusher, Policy: policy, Rotations: rotations, Metrics: metrics}
	if deps.OIDC.Enabled {
		if f.Login, err = newOIDCLogin(deps.Pools, deps.OIDC, deps.Server); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// minDevicePollInterval is the request spacing the limiter of the /oidc
// routes allows: RATE_LIMIT_ANONYMOUS with RATE_LIMIT_SPLIT, the shared
// legacy limiter without. An invalid RATE_LIMIT_ANONYMOUS stops NewServer.
func minDevicePollInterval(srv env.Server) time.Duration {
	rate := middleware.LegacyRate
	if srv.RateLimitSplit {
		parsed, err := middleware.ParseRate(cmp.Or(srv.RateLimitAnonymous, "10/1m"))
		if err != nil {
			return 0
		}
		rate = parsed
	}
	return rate.Window / time.Duration(rate.Requests)
}

// newOIDCLogin discovers the provider and stores users and login states on
// Pools.API.
func newOIDCLogin(pools *dbpool.Pools, cfg env.OIDC, srv env.Server) (*authn.OIDCLogin, error) {
	authCfg := authn.OIDCConfig{
		Enabled: true, IssuerURL: cfg.IssuerURL, ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
		Scopes: cfg.Scopes, UsernameClaim: cfg.UsernameClaim, GroupsClaim: cfg.GroupsClaim, RoleMap: cfg.RoleMap,
		LoginStateTTL: cmp.Or(cfg.LoginStateTTL, 10*time.Minute), MinDevicePollInterval: minDevicePollInterval(srv),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := oidc.New(ctx, authCfg)
	if err != nil {
		return nil, err
	}
	store, err := postgres.NewIdentityStore(pools.API)
	if err != nil {
		return nil, err
	}
	return authn.NewOIDCLogin(provider, store, fleet.SystemClock{}, authCfg)
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

// NewSealer seals stored secrets with DATA_ENCRYPTION_KEY, which robot
// rotation and webhooks require.
func NewSealer(key string) (*secrets.AESGCM, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: DATA_ENCRYPTION_KEY is not set", fleet.ErrInvalid)
	}
	raw, err := secrets.ParseKey(key)
	if err != nil {
		return nil, err
	}
	return secrets.NewAESGCM(raw)
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
	r, err := harbor2.New(harbor2.Config{URL: h.URL, Username: h.Username, Password: h.Password, Insecure: strings.HasPrefix(h.URL, "http://")})
	if err != nil {
		return nil, fmt.Errorf("HARBOR_URL must be the absolute URL of Harbor: %w", err)
	}
	return r, nil
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
// (server.WithSatelliteAuthenticator(f.Sessions)), the request metrics
// (server.WithRequestObserver), when set, the policy
// (server.WithPolicy(f.Policy)) and, with OIDC, the operator login
// (server.WithOperatorLogin).
func (f *Fleet) ServerOptions(pools *dbpool.Pools) []server.Option {
	opts := []server.Option{server.WithFleet(f.Services)}
	if f.Metrics != nil {
		opts = append(opts, server.WithRequestObserver(f.Metrics.Recorder()))
	}
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
		HeartbeatFlushInterval: withHotPathDefaults(srv.FleetHotPath).HeartbeatFlushInterval,
		WebhooksEnabled:        webhooks.Enabled,
	}
}
