package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.SessionIssuer = (*SessionIssuer)(nil)

// SessionIssuer is a recording fleet.SessionIssuer; its calls are recorded
// as "Sessions.<Method>".
type SessionIssuer struct {
	*Recorder
	// Grant is what Issue, IssueIfNone and Renew return.
	Grant fleet.SessionGrant
	// IssuedIfNone and Renewed are the ok results of IssueIfNone and Renew.
	IssuedIfNone, Renewed bool
}

// NewSessionIssuer records into rec, or into a new Recorder when rec is
// nil.
func NewSessionIssuer(rec *Recorder) *SessionIssuer {
	return &SessionIssuer{Recorder: orNew(rec)}
}

func (s *SessionIssuer) Issue(_ context.Context, satellite fleet.SatelliteID) (fleet.SessionGrant, error) {
	if err := s.Record("Sessions.Issue", satellite); err != nil {
		return fleet.SessionGrant{}, err
	}
	return s.Grant, nil
}

func (s *SessionIssuer) IssueIfNone(_ context.Context, satellite fleet.SatelliteID) (fleet.SessionGrant, bool, error) {
	if err := s.Record("Sessions.IssueIfNone", satellite); err != nil {
		return fleet.SessionGrant{}, false, err
	}
	if !s.IssuedIfNone {
		return fleet.SessionGrant{}, false, nil
	}
	return s.Grant, true, nil
}

func (s *SessionIssuer) Renew(_ context.Context, auth fleet.SatelliteAuth) (fleet.SessionGrant, bool, error) {
	if err := s.Record("Sessions.Renew", auth); err != nil {
		return fleet.SessionGrant{}, false, err
	}
	if !s.Renewed {
		return fleet.SessionGrant{}, false, nil
	}
	return s.Grant, true, nil
}

func (s *SessionIssuer) Invalidate(_ context.Context, satellite fleet.SatelliteID) error {
	return s.Record("Sessions.Invalidate", satellite)
}
