package fleet

import (
	"context"
	"time"
)

// LifecycleState is a satellite's state of ADR-0006 (plan item 4.3). A
// satellite without a satellite_view row is pending.
type LifecycleState string

const (
	StatePending LifecycleState = "pending"
	StateOnline  LifecycleState = "online"
	StateStale   LifecycleState = "stale"
	StateRevoked LifecycleState = "revoked"
)

// SatelliteStale is log-only; Revoked and Unrevoked make the satellite
// reconciler disable or enable the robot.
const (
	EventSatelliteStale     EventKind = "SatelliteStale"
	EventSatelliteRevoked   EventKind = "SatelliteRevoked"
	EventSatelliteUnrevoked EventKind = "SatelliteUnrevoked"
)

type SatelliteLifecyclePayload struct {
	Name          string         `json:"name"`
	State         LifecycleState `json:"state"`
	PreviousState LifecycleState `json:"previous_state"`
	LastSeen      time.Time      `json:"last_seen,omitzero"`
}

// AppliedState is a group state a satellite reports applied: its
// reference as the satellite state lists it and the digest it applied.
type AppliedState struct {
	Ref    string `json:"ref"`
	Digest Digest `json:"digest"`
}

// PublishedGroup is a group of the satellite and the artifact digest GC
// published for its state last. Ref is filled by SatelliteService from
// Settings.GroupStateRef(Group).String().
type PublishedGroup struct {
	Group          string
	Ref            string
	ArtifactDigest Digest
}

type SatelliteView struct {
	Satellite                               Satellite
	State                                   LifecycleState
	LastActivity                            string
	LastReportedAt                          time.Time
	AppliedStateDigest, AppliedConfigDigest Digest
	AppliedStates                           []AppliedState
	PublishedGroups                         []PublishedGroup
	InventoryDigest                         Digest
	LastReplicationError                    string
	RevokedAt                               time.Time
	DesiredVersion                          int64
	RenderedDigest, PublishedDigest         Digest
	PublishedArtifactDigest                 Digest
	PublishError                            string
	PublishedAt, UpdatedAt                  time.Time
}

// Converged reports whether the satellite applied everything GC published
// for it: its own state artifact by digest, and every group by ref, where a
// group not yet published, not reported or reported with another digest
// is not converged.
func (v SatelliteView) Converged() bool {
	if v.AppliedStateDigest == "" || v.AppliedStateDigest != v.PublishedArtifactDigest {
		return false
	}
	applied := make(map[string]Digest, len(v.AppliedStates))
	for _, a := range v.AppliedStates {
		applied[a.Ref] = a.Digest
	}
	for _, g := range v.PublishedGroups {
		if g.ArtifactDigest == "" || applied[g.Ref] != g.ArtifactDigest {
			return false
		}
	}
	return true
}

type StaleSatellite struct {
	Satellite SatelliteID
	Name      string
	LastSeen  time.Time
}

// ViewRepo is the satellite_view read model. Lookups of an unknown
// satellite are ErrNotFound; a known satellite without a view row is
// pending.
type ViewRepo interface {
	Get(ctx context.Context, id SatelliteID) (SatelliteView, error)
	GetByName(ctx context.Context, name string) (SatelliteView, error)
	ListByState(ctx context.Context, state LifecycleState) ([]SatelliteView, error)
	Revoke(ctx context.Context, id SatelliteID, now time.Time) (previous LifecycleState, err error)
	Unrevoke(ctx context.Context, id SatelliteID, now time.Time) (changed bool, err error)
	// MarkStale takes advisorylock.StaleDetector in the caller's
	// transaction and returns nothing while another replica holds it.
	MarkStale(ctx context.Context, now time.Time, defaultThreshold, minThreshold time.Duration) ([]StaleSatellite, error)
	Inventory(ctx context.Context, id SatelliteID) ([]InventoryItem, error)
}
