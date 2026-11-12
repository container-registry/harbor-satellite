package fleet

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type SatelliteService struct{}

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
	return RegistrationToken{}, notimpl.Err("1.2 register satellite")
}

// Delete appends the tombstone in the satellite's transaction, with
// RobotHarborIDs from Robots().HarborIDs so an unpromoted successor robot
// (plan item 8.4) is deleted too, and, after
// the commit, invalidates its sessions (Sessions.Invalidate), so the
// deleted satellite gets 401 within the session cache TTL.
func (s *SatelliteService) Delete(ctx context.Context, name string) error {
	return notimpl.Err("1.2 delete satellite")
}

func (s *SatelliteService) Get(ctx context.Context, name string) (Satellite, error) {
	return Satellite{}, notimpl.Err("1.2 get satellite")
}

func (s *SatelliteService) List(ctx context.Context) ([]Satellite, error) {
	return nil, notimpl.Err("1.2 list satellites")
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
