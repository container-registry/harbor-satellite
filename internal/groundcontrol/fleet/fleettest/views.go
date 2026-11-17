package fleettest

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.ViewRepo = (*ViewRepo)(nil)

// ViewRepo is a recording fleet.ViewRepo; its calls are recorded as
// "Views.<Method>".
type ViewRepo struct {
	rec *Recorder
	// ByID answers Get, GetByName and ListByState; a satellite without an
	// entry is fleet.ErrNotFound.
	ByID map[fleet.SatelliteID]fleet.SatelliteView
	// Previous is what Revoke returns, Changed what Unrevoke returns.
	Previous fleet.LifecycleState
	Changed  bool
	// Stale is what MarkStale returns.
	Stale []fleet.StaleSatellite
	// InventoryOf answers Inventory.
	InventoryOf map[fleet.SatelliteID][]fleet.InventoryItem
}

func (v *ViewRepo) Get(_ context.Context, id fleet.SatelliteID) (fleet.SatelliteView, error) {
	return lookup(v.rec, "Views.Get", v.ByID, id)
}

func (v *ViewRepo) GetByName(_ context.Context, name string) (fleet.SatelliteView, error) {
	return find(v.rec, "Views.GetByName", name, v.ByID, func(sv fleet.SatelliteView) bool { return sv.Satellite.Name == name })
}

// ListByState returns the views in state ordered by satellite ID.
func (v *ViewRepo) ListByState(_ context.Context, state fleet.LifecycleState) ([]fleet.SatelliteView, error) {
	if err := v.rec.Record("Views.ListByState", state); err != nil {
		return nil, err
	}
	var out []fleet.SatelliteView
	for _, sv := range v.ByID {
		if sv.State == state {
			out = append(out, sv)
		}
	}
	sortViews(out)
	return out, nil
}

func (v *ViewRepo) Revoke(_ context.Context, id fleet.SatelliteID, now time.Time) (fleet.LifecycleState, error) {
	if err := v.rec.Record("Views.Revoke", id, now); err != nil {
		return "", err
	}
	return v.Previous, nil
}

func (v *ViewRepo) Unrevoke(_ context.Context, id fleet.SatelliteID, now time.Time) (bool, error) {
	if err := v.rec.Record("Views.Unrevoke", id, now); err != nil {
		return false, err
	}
	return v.Changed, nil
}

func (v *ViewRepo) MarkStale(_ context.Context, now time.Time, defaultThreshold, minThreshold time.Duration) ([]fleet.StaleSatellite, error) {
	if err := v.rec.Record("Views.MarkStale", now, defaultThreshold, minThreshold); err != nil {
		return nil, err
	}
	return v.Stale, nil
}

func (v *ViewRepo) Inventory(_ context.Context, id fleet.SatelliteID) ([]fleet.InventoryItem, error) {
	if err := v.rec.Record("Views.Inventory", id); err != nil {
		return nil, err
	}
	return v.InventoryOf[id], nil
}

func sortViews(views []fleet.SatelliteView) {
	for i := 1; i < len(views); i++ {
		for j := i; j > 0 && views[j].Satellite.ID < views[j-1].Satellite.ID; j-- {
			views[j], views[j-1] = views[j-1], views[j]
		}
	}
}
