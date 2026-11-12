package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// LimitRegistry spaces the calls to registry at no more than rps per
// second (HARBOR_RATE_LIMIT) for this process, so a backlog of events
// cannot flood Harbor. The skeleton returns registry unchanged.
func LimitRegistry(registry fleet.Registry, rps float64) fleet.Registry {
	return registry
}

var _ Reachability = (*RegistryProbe)(nil)

// RegistryProbe pings the registry every interval and remembers the last
// successful ping. Every serve process runs one, with --worker=off too:
// /health reports Harbor as degraded from it (plan item 2.6) and the
// publisher pauses on it. It pings the unlimited registry, so a busy rate
// limit cannot make Harbor look down.
type RegistryProbe struct{}

// NewRegistryProbe considers the registry unreachable when the last
// successful ping is older than staleAfter.
func NewRegistryProbe(registry fleet.Registry, interval, staleAfter time.Duration) (*RegistryProbe, error) {
	return nil, notimpl.Err("2.6 registry probe")
}

func (p *RegistryProbe) Name() string { return "registry-probe" }

func (p *RegistryProbe) Run(ctx context.Context) error {
	return notimpl.Err("2.6 registry probe run")
}

// LastContact is the time of the last successful ping; zero before the
// first one.
func (p *RegistryProbe) LastContact() time.Time {
	return time.Time{}
}

func (p *RegistryProbe) Reachable() bool {
	return false
}
