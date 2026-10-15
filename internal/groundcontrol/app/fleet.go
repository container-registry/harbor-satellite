// Package app is Ground Control's composition root: it builds the
// adapters and hands them to the fleet services. `serve` uses it once the
// handlers call the services (plan item 1.4); until then nothing does.
package app

import (
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type FleetDeps struct {
	// Pools: admin writes run on Pools.API, so a busy fleet plane
	// cannot starve them.
	Pools  *dbpool.Pools
	Harbor env.Harbor
	// SPIRE is nil when no SPIRE server is configured; SPIFFE
	// registration then fails.
	SPIRE *spiffe.ServerClient
}

// NewFleet builds the services over postgres.Repository, a harbor2.Registry
// configured from deps.Harbor and, with SPIRE, a spire.WorkloadIdentity,
// with Settings(deps.Harbor).
func NewFleet(deps FleetDeps) (*fleet.Services, error) {
	return nil, notimpl.Err("1.3 fleet composition")
}

// Settings maps the Harbor environment to the fleet settings; every
// composition uses it, so the services and the worker agree.
func Settings(h env.Harbor) fleet.Settings {
	return fleet.Settings{
		RegistryURL:   h.URL,
		RobotDuration: time.Duration(h.RobotDurationDaysValue()) * 24 * time.Hour,
		TokenTTL:      fleet.DefaultTokenTTL,
	}
}
