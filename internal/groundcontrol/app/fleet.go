// Package app is Ground Control's composition root: it builds the
// adapters, hands them to the fleet services and the async plane, and
// gives the server what it needs as options, so the server never imports
// the async plane. `serve` and `worker` use it once the handlers call the
// services (plan item 1.4); until then nothing does.
package app

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/casbin"
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
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
func NewFleet(deps FleetDeps) (*Fleet, error) {
	return nil, notimpl.Err("1.3 fleet composition")
}

// ServerOptions hands the server the services (server.WithFleet), the
// probe (server.WithHarborContact), the outbox reader for
// /api/ops/outbox (server.WithOutboxReader), the sessions
// (server.WithSatelliteAuthenticator(f.Sessions)), when set, the policy
// (server.WithPolicy(f.Policy)) and, with OIDC, the operator login
// (server.WithOperatorLogin).
func (f *Fleet) ServerOptions(pools *dbpool.Pools) []server.Option {
	return nil
}

// Settings maps the Harbor, server and webhook environment to the fleet
// settings; every composition uses it, so the services and the worker agree.
func Settings(h env.Harbor, srv env.Server, webhooks env.Webhook) fleet.Settings {
	return fleet.Settings{
		RegistryURL:            h.URL,
		RobotDuration:          time.Duration(h.RobotDurationDaysValue()) * 24 * time.Hour,
		TokenTTL:               fleet.DefaultTokenTTL,
		StaleThreshold:         srv.StaleThreshold,
		HeartbeatFlushInterval: srv.HeartbeatFlushInterval,
		WebhooksEnabled:        webhooks.Enabled,
	}
}
