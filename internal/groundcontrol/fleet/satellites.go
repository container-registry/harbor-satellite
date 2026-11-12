package fleet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
	sat, err := s.c.read().Satellites().GetByName(ctx, name)
	if err != nil {
		return Satellite{}, fmt.Errorf("satellite %s: %w", name, err)
	}
	return sat, nil
}

func (s *SatelliteService) List(ctx context.Context) ([]Satellite, error) {
	return s.c.read().Satellites().List(ctx)
}

// Revoke runs one transaction: Views().Revoke, then SatelliteRevoked only
// when the state changed. Sessions are kept: a revoked satellite still
// authenticates and gets 403, and ADR-0006's re-enable needs no new
// onboarding.
func (s *SatelliteService) Revoke(ctx context.Context, name string) (SatelliteView, error) {
	return SatelliteView{}, notimpl.Err("4.3 revoke satellite")
}

// Unrevoke returns a revoked satellite to pending and appends
// SatelliteUnrevoked; a satellite that is not revoked is left alone.
func (s *SatelliteService) Unrevoke(ctx context.Context, name string) (SatelliteView, error) {
	return SatelliteView{}, notimpl.Err("4.3 unrevoke satellite")
}

// View is the satellite's lifecycle and convergence; it fills the
// PublishedGroups refs from Settings.GroupStateRef.
func (s *SatelliteService) View(ctx context.Context, name string) (SatelliteView, error) {
	return SatelliteView{}, notimpl.Err("4.3 satellite view")
}

func (s *SatelliteService) ListByState(ctx context.Context, state LifecycleState) ([]SatelliteView, error) {
	return nil, notimpl.Err("4.3 list satellites by state")
}

// Inventory is the satellite's last reported inventory (plan item 4.4).
func (s *SatelliteService) Inventory(ctx context.Context, name string) ([]InventoryItem, error) {
	return nil, notimpl.Err("4.4 satellite inventory")
}
