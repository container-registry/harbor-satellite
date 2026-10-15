package postgres

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// viewRepo runs the satellite_view queries: GetSatelliteViewByID,
// GetSatelliteViewByName, ListSatelliteViewsByState, RevokeSatelliteView,
// UnrevokeSatelliteView, MarkStaleSatellites (after
// TryAdvisoryXactLock(advisorylock.StaleDetector) in the same transaction)
// and ListSatelliteInventory.
type viewRepo struct{}

func (viewRepo) Get(ctx context.Context, id fleet.SatelliteID) (fleet.SatelliteView, error) {
	return fleet.SatelliteView{}, notimpl.Err("4.3 postgres satellite view")
}

func (viewRepo) GetByName(ctx context.Context, name string) (fleet.SatelliteView, error) {
	return fleet.SatelliteView{}, notimpl.Err("4.3 postgres satellite view by name")
}

func (viewRepo) ListByState(ctx context.Context, state fleet.LifecycleState) ([]fleet.SatelliteView, error) {
	return nil, notimpl.Err("4.3 postgres satellite views by state")
}

func (viewRepo) Revoke(ctx context.Context, id fleet.SatelliteID, now time.Time) (fleet.LifecycleState, error) {
	return "", notimpl.Err("4.3 postgres revoke satellite")
}

func (viewRepo) Unrevoke(ctx context.Context, id fleet.SatelliteID, now time.Time) (bool, error) {
	return false, notimpl.Err("4.3 postgres unrevoke satellite")
}

func (viewRepo) MarkStale(ctx context.Context, now time.Time, defaultThreshold, minThreshold time.Duration) ([]fleet.StaleSatellite, error) {
	return nil, notimpl.Err("4.3 postgres mark stale satellites")
}

func (viewRepo) Inventory(ctx context.Context, id fleet.SatelliteID) ([]fleet.InventoryItem, error) {
	return nil, notimpl.Err("4.4 postgres satellite inventory")
}
