// Package app is Ground Control's composition root: it builds the
// adapters, hands them to the fleet services and the async plane, and
// gives the server what it needs as options, so the server never imports
// the async plane. `serve` and `worker` use it once the handlers call the
// services (plan item 1.4); until then nothing does.
package app

import (
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
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
)

type FleetDeps struct {
	// Pools: admin writes run on Pools.API, so a busy fleet plane
	// cannot starve them.
	Pools  *dbpool.Pools
	Harbor env.Harbor
	// Server carries the session, heartbeat and stale settings of plan
	// item 4.
	Server env.Server
	// SPIRE is nil when no SPIRE server is configured; SPIFFE
	// registration then fails.
	SPIRE *spiffe.ServerClient
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
}

// NewFleet builds the services over postgres.Repository, a harbor2.Registry
// configured from deps.Harbor and, with SPIRE, a spire.WorkloadIdentity,
// with Settings(deps.Harbor, deps.Server). The probe pings the same
// registry without the rate limit every HARBOR_PROBE_INTERVAL and counts
// it stale after three intervals. Sessions run on a postgres.SessionStore
// and the flusher on a postgres.HeartbeatStore, both on Pools.Fleet, which
// is also the services' SatelliteRepository.
func NewFleet(deps FleetDeps) (*Fleet, error) {
	return nil, notimpl.Err("1.3 fleet composition")
}

// ServerOptions hands the server the services (server.WithFleet), the
// probe (server.WithHarborContact), the outbox reader for
// /api/ops/outbox (server.WithOutboxReader) and the sessions
// (server.WithSatelliteAuthenticator(f.Sessions)).
func (f *Fleet) ServerOptions(pools *dbpool.Pools) []server.Option {
	return nil
}

// Settings maps the Harbor and server environment to the fleet settings;
// every composition uses it, so the services and the worker agree.
func Settings(h env.Harbor, srv env.Server) fleet.Settings {
	return fleet.Settings{
		RegistryURL:            h.URL,
		RobotDuration:          time.Duration(h.RobotDurationDaysValue()) * 24 * time.Hour,
		TokenTTL:               fleet.DefaultTokenTTL,
		StaleThreshold:         srv.StaleThreshold,
		HeartbeatFlushInterval: srv.HeartbeatFlushInterval,
	}
}
