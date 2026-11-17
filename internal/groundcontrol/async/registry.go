package async

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// LimitRegistry spaces the calls to registry at no more than rps per
// second (HARBOR_RATE_LIMIT) for this process, so a backlog of events
// cannot flood Harbor. An rps of zero or less returns registry unchanged.
func LimitRegistry(registry fleet.Registry, rps float64) fleet.Registry {
	if rps <= 0 || registry == nil {
		return registry
	}
	return &limitedRegistry{Registry: registry, interval: time.Duration(float64(time.Second) / rps)}
}

type limitedRegistry struct {
	fleet.Registry
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

// wait reserves the next slot and sleeps until it.
func (l *limitedRegistry) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	slot := l.next
	if slot.Before(now) {
		slot = now
	}
	l.next = slot.Add(l.interval)
	l.mu.Unlock()
	delay := time.Until(slot)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *limitedRegistry) FindRobot(ctx context.Context, name string) (fleet.RobotCredentials, error) {
	if err := l.wait(ctx); err != nil {
		return fleet.RobotCredentials{}, err
	}
	return l.Registry.FindRobot(ctx, name)
}

func (l *limitedRegistry) CreateRobot(ctx context.Context, spec fleet.RobotSpec) (fleet.RobotCredentials, error) {
	if err := l.wait(ctx); err != nil {
		return fleet.RobotCredentials{}, err
	}
	return l.Registry.CreateRobot(ctx, spec)
}

func (l *limitedRegistry) SetRobotAccess(ctx context.Context, harborID int64, projects []string, disabled bool) error {
	if err := l.wait(ctx); err != nil {
		return err
	}
	return l.Registry.SetRobotAccess(ctx, harborID, projects, disabled)
}

func (l *limitedRegistry) RotateRobotSecret(ctx context.Context, harborID int64) (fleet.RobotCredentials, error) {
	if err := l.wait(ctx); err != nil {
		return fleet.RobotCredentials{}, err
	}
	return l.Registry.RotateRobotSecret(ctx, harborID)
}

func (l *limitedRegistry) DeleteRobot(ctx context.Context, harborID int64) error {
	if err := l.wait(ctx); err != nil {
		return err
	}
	return l.Registry.DeleteRobot(ctx, harborID)
}

func (l *limitedRegistry) PushState(ctx context.Context, ref fleet.ArtifactRef, payload []byte) (fleet.Digest, error) {
	if err := l.wait(ctx); err != nil {
		return "", err
	}
	return l.Registry.PushState(ctx, ref, payload)
}

func (l *limitedRegistry) DeleteState(ctx context.Context, ref fleet.ArtifactRef) error {
	if err := l.wait(ctx); err != nil {
		return err
	}
	return l.Registry.DeleteState(ctx, ref)
}

func (l *limitedRegistry) EnsureProject(ctx context.Context, project string) error {
	if err := l.wait(ctx); err != nil {
		return err
	}
	return l.Registry.EnsureProject(ctx, project)
}

func (l *limitedRegistry) ReplicationPolicyExists(ctx context.Context, name string) (bool, error) {
	if err := l.wait(ctx); err != nil {
		return false, err
	}
	return l.Registry.ReplicationPolicyExists(ctx, name)
}

func (l *limitedRegistry) Ping(ctx context.Context) error {
	if err := l.wait(ctx); err != nil {
		return err
	}
	return l.Registry.Ping(ctx)
}

var _ Reachability = (*RegistryProbe)(nil)

// RegistryProbe pings the registry every interval and remembers the last
// successful ping. Every serve process runs one, with --worker=off too:
// /health reports Harbor as degraded from it (plan item 2.6) and the
// publisher pauses on it. It pings the unlimited registry, so a busy rate
// limit cannot make Harbor look down.
type RegistryProbe struct {
	registry             fleet.Registry
	interval, staleAfter time.Duration
	mu                   sync.Mutex
	last                 time.Time
	failing              bool
}

// NewRegistryProbe considers the registry unreachable when the last
// successful ping is older than staleAfter.
func NewRegistryProbe(registry fleet.Registry, interval, staleAfter time.Duration) (*RegistryProbe, error) {
	if registry == nil || interval <= 0 || staleAfter <= 0 {
		return nil, fmt.Errorf("%w: the registry probe needs a registry and positive intervals", fleet.ErrInvalid)
	}
	return &RegistryProbe{registry: registry, interval: interval, staleAfter: staleAfter}, nil
}

func (p *RegistryProbe) Name() string { return "registry-probe" }

func (p *RegistryProbe) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		p.ping(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (p *RegistryProbe) ping(ctx context.Context) {
	pingCtx, cancel := context.WithTimeout(ctx, max(p.interval, 5*time.Second))
	defer cancel()
	err := p.registry.Ping(pingCtx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		if p.failing {
			log.Printf("registry probe: registry reachable again")
		}
		p.last, p.failing = time.Now(), false
		return
	}
	if !p.failing && ctx.Err() == nil {
		log.Printf("registry probe: %v", err)
	}
	p.failing = true
}

// LastContact is the time of the last successful ping; zero before the
// first one.
func (p *RegistryProbe) LastContact() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

func (p *RegistryProbe) Reachable() bool {
	last := p.LastContact()
	return !last.IsZero() && time.Since(last) <= p.staleAfter
}

// StaleAfter is how old the last contact may be while the registry counts
// as reachable.
func (p *RegistryProbe) StaleAfter() time.Duration { return p.staleAfter }
