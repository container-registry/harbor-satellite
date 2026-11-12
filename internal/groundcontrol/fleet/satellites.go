package fleet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type SatelliteService struct{ c *core }

type RegisterSatelliteCmd struct {
	Name, ConfigName string
	Groups           []string
}

type RegistrationToken struct {
	Token     string
	ExpiresAt time.Time
}

// Register creates a token satellite in its groups with its config, a
// robot that may pull the union of the groups' projects and the satellite
// project, and its state artifact, groupless included (D10). Any failure
// rolls the repository back and deletes a robot it created. A satellite or
// robot that already exists is ErrAlreadyExists.
func (s *SatelliteService) Register(ctx context.Context, cmd RegisterSatelliteCmd) (RegistrationToken, error) {
	if !ValidName(cmd.Name) {
		return RegistrationToken{}, fmt.Errorf("%w: satellite name %q", ErrInvalid, cmd.Name)
	}
	if !ValidName(cmd.ConfigName) {
		return RegistrationToken{}, fmt.Errorf("%w: config name %q", ErrInvalid, cmd.ConfigName)
	}
	checks := []check{{ActionRegister, satelliteResource(cmd.Name, cmd.Groups)}}
	for _, g := range cmd.Groups {
		checks = append(checks, check{ActionAddMember, Resource{Type: ResourceGroup, ID: g}})
	}
	if err := s.c.authorizeAll(ctx, checks...); err != nil {
		return RegistrationToken{}, err
	}
	token, err := newToken()
	if err != nil {
		return RegistrationToken{}, err
	}
	expires := s.c.clock.Now().Add(s.c.settings.tokenTTL())
	err = s.c.inTx(ctx, func(tx Tx) error {
		sat, err := tx.Satellites().Create(ctx, cmd.Name)
		if err != nil {
			return fmt.Errorf("create satellite %s: %w", cmd.Name, err)
		}
		if err := addToGroups(ctx, tx, sat.ID, cmd.Groups); err != nil {
			return err
		}
		cfg, err := tx.Configs().GetByName(ctx, cmd.ConfigName)
		if err != nil {
			return fmt.Errorf("config %s: %w", cmd.ConfigName, err)
		}
		if err := tx.Satellites().SetConfig(ctx, sat.ID, cfg.ID); err != nil {
			return fmt.Errorf("assign config %s: %w", cmd.ConfigName, err)
		}
		if err := tx.Tokens().Add(ctx, sat.ID, token, expires); err != nil {
			return fmt.Errorf("store registration token: %w", err)
		}
		return tx.Events().Append(ctx, satelliteEvent(EventSatelliteCreated, sat))
	})
	if err != nil {
		return RegistrationToken{}, err
	}
	return RegistrationToken{Token: token, ExpiresAt: expires}, nil
}

func addToGroups(ctx context.Context, tx Tx, satellite SatelliteID, groups []string) error {
	for _, name := range groups {
		group, err := tx.Groups().GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("group %s: %w", name, err)
		}
		if err := tx.Groups().AddMember(ctx, group.ID, satellite); err != nil {
			return fmt.Errorf("add satellite to group %s: %w", name, err)
		}
	}
	return nil
}

func satelliteEvent(kind EventKind, sat Satellite) Event {
	return Event{Kind: kind, Aggregate: Aggregate{Type: AggregateSatellite, ID: int64(sat.ID)}, Payload: ChangedPayload{Name: sat.Name}}
}

// newToken is 32 hex characters, the format satellites expect.
func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate registration token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Delete appends the tombstone in the satellite's transaction, with
// RobotHarborIDs from Robots().HarborIDs so an unpromoted successor robot
// (plan item 8.4) is deleted too, and, after
// the commit, invalidates its sessions (Sessions.Invalidate), so the
// deleted satellite gets 401 within the session cache TTL.
func (s *SatelliteService) Delete(ctx context.Context, name string) error {
	stateRef, err := s.c.settings.SatelliteStateRef(name)
	if err != nil {
		return err
	}
	if err := s.authorizeSatellite(ctx, ActionDelete, name); err != nil {
		return err
	}
	var id SatelliteID
	err = s.c.inTx(ctx, func(tx Tx) error {
		sat, err := tx.Satellites().GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("satellite %s: %w", name, err)
		}
		id = sat.ID
		robots, err := tx.Robots().HarborIDs(ctx, sat.ID)
		if err != nil {
			return fmt.Errorf("robots of satellite %s: %w", name, err)
		}
		if err := tx.Satellites().Delete(ctx, sat.ID); err != nil {
			return fmt.Errorf("delete satellite %s: %w", name, err)
		}
		if robots == nil {
			robots = []int64{}
		}
		return tx.Events().Append(ctx, Event{
			Kind:      EventSatelliteDeleted,
			Aggregate: Aggregate{Type: AggregateSatellite, ID: int64(sat.ID)},
			Payload:   SatelliteDeletedPayload{Name: sat.Name, RobotHarborIDs: robots, SPIFFEID: sat.SPIFFEID, StateRef: stateRef},
		})
	})
	if err != nil {
		return err
	}
	if s.c.sessions != nil {
		if err := s.c.sessions.Invalidate(ctx, id); err != nil {
			return fmt.Errorf("invalidate sessions of satellite %s: %w", name, err)
		}
	}
	return nil
}

func (s *SatelliteService) Get(ctx context.Context, name string) (Satellite, error) {
	sat, groups, err := s.c.satelliteScope(ctx, s.c.read(), name)
	if err != nil {
		return Satellite{}, err
	}
	if err := s.c.authorize(ctx, ActionRead, satelliteResource(name, groups)); err != nil {
		return Satellite{}, err
	}
	return sat, nil
}

func (s *SatelliteService) List(ctx context.Context) ([]Satellite, error) {
	sats, err := s.c.read().Satellites().List(ctx)
	if err != nil {
		return nil, err
	}
	return s.c.readableSatellites(ctx, sats)
}

// Revoke runs one transaction: Views().Revoke, then SatelliteRevoked only
// when the state changed. Sessions are kept: a revoked satellite still
// authenticates and gets 403, and ADR-0006's re-enable needs no new
// onboarding.
func (s *SatelliteService) Revoke(ctx context.Context, name string) (SatelliteView, error) {
	return s.transition(ctx, ActionRevoke, name, func(tx Tx, sat Satellite, now time.Time) (*Event, error) {
		previous, err := tx.Views().Revoke(ctx, sat.ID, now)
		if err != nil || previous == StateRevoked {
			return nil, err
		}
		return &Event{
			Kind:      EventSatelliteRevoked,
			Aggregate: Aggregate{Type: AggregateSatellite, ID: int64(sat.ID)},
			Payload:   SatelliteLifecyclePayload{Name: sat.Name, State: StateRevoked, PreviousState: previous, LastSeen: sat.LastSeen},
		}, nil
	})
}

// Unrevoke returns a revoked satellite to pending and appends
// SatelliteUnrevoked; a satellite that is not revoked is left alone.
func (s *SatelliteService) Unrevoke(ctx context.Context, name string) (SatelliteView, error) {
	return s.transition(ctx, ActionUnrevoke, name, func(tx Tx, sat Satellite, now time.Time) (*Event, error) {
		changed, err := tx.Views().Unrevoke(ctx, sat.ID, now)
		if err != nil || !changed {
			return nil, err
		}
		return &Event{
			Kind:      EventSatelliteUnrevoked,
			Aggregate: Aggregate{Type: AggregateSatellite, ID: int64(sat.ID)},
			Payload:   SatelliteLifecyclePayload{Name: sat.Name, State: StatePending, PreviousState: StateRevoked, LastSeen: sat.LastSeen},
		}, nil
	})
}

// transition runs change on the named satellite in one transaction,
// appends the event it returns and answers the view after the change.
func (s *SatelliteService) transition(ctx context.Context, action Action, name string, change func(Tx, Satellite, time.Time) (*Event, error)) (SatelliteView, error) {
	if err := s.authorizeSatellite(ctx, action, name); err != nil {
		return SatelliteView{}, err
	}
	var view SatelliteView
	err := s.c.inTx(ctx, func(tx Tx) error {
		sat, err := tx.Satellites().GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("satellite %s: %w", name, err)
		}
		event, err := change(tx, sat, s.c.clock.Now())
		if err != nil {
			return fmt.Errorf("satellite %s: %w", name, err)
		}
		if event != nil {
			if err := tx.Events().Append(ctx, *event); err != nil {
				return err
			}
		}
		view, err = tx.Views().Get(ctx, sat.ID)
		return err
	})
	if err != nil {
		return SatelliteView{}, err
	}
	return s.withRefs(view), nil
}

// View is the satellite's lifecycle and convergence; it fills the
// PublishedGroups refs from Settings.GroupStateRef.
func (s *SatelliteService) View(ctx context.Context, name string) (SatelliteView, error) {
	if err := s.authorizeSatellite(ctx, ActionRead, name); err != nil {
		return SatelliteView{}, err
	}
	view, err := s.c.read().Views().GetByName(ctx, name)
	if err != nil {
		return SatelliteView{}, fmt.Errorf("satellite %s: %w", name, err)
	}
	return s.withRefs(view), nil
}

func (s *SatelliteService) ListByState(ctx context.Context, state LifecycleState) ([]SatelliteView, error) {
	switch state {
	case StatePending, StateOnline, StateStale, StateRevoked:
	default:
		return nil, fmt.Errorf("%w: lifecycle state %q", ErrInvalid, state)
	}
	views, err := s.c.read().Views().ListByState(ctx, state)
	if err != nil {
		return nil, err
	}
	for i := range views {
		views[i] = s.withRefs(views[i])
	}
	return s.readable(ctx, views)
}

// Inventory is the satellite's last reported inventory (plan item 4.4).
func (s *SatelliteService) Inventory(ctx context.Context, name string) ([]InventoryItem, error) {
	if err := s.authorizeSatellite(ctx, ActionRead, name); err != nil {
		return nil, err
	}
	tx := s.c.read()
	sat, err := tx.Satellites().GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("satellite %s: %w", name, err)
	}
	return tx.Views().Inventory(ctx, sat.ID)
}

func (s *SatelliteService) withRefs(view SatelliteView) SatelliteView {
	for i, g := range view.PublishedGroups {
		if ref, err := s.c.settings.GroupStateRef(g.Group); err == nil {
			view.PublishedGroups[i].Ref = ref.String()
		}
	}
	return view
}
