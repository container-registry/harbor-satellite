package postgres

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type satelliteRepo struct{}

func (satelliteRepo) Create(ctx context.Context, name string) (fleet.Satellite, error) {
	return fleet.Satellite{}, notimpl.Err("1.3 postgres create satellite")
}

func (satelliteRepo) Get(ctx context.Context, id fleet.SatelliteID) (fleet.Satellite, error) {
	return fleet.Satellite{}, notimpl.Err("1.3 postgres get satellite")
}

func (satelliteRepo) GetByName(ctx context.Context, name string) (fleet.Satellite, error) {
	return fleet.Satellite{}, notimpl.Err("1.3 postgres get satellite by name")
}

func (satelliteRepo) List(ctx context.Context) ([]fleet.Satellite, error) {
	return nil, notimpl.Err("1.3 postgres list satellites")
}

func (satelliteRepo) Delete(ctx context.Context, id fleet.SatelliteID) error {
	return notimpl.Err("1.3 postgres delete satellite")
}

func (satelliteRepo) Groups(ctx context.Context, id fleet.SatelliteID) ([]fleet.Group, error) {
	return nil, notimpl.Err("1.3 postgres satellite groups")
}

// RobotGrants runs the set-based GetProjectsOfSatellites of D5.
func (satelliteRepo) RobotGrants(ctx context.Context, ids []fleet.SatelliteID) ([]fleet.RobotGrant, error) {
	return nil, notimpl.Err("1.3 postgres robot grants")
}

func (satelliteRepo) SetConfig(ctx context.Context, id fleet.SatelliteID, config fleet.ConfigID) error {
	return notimpl.Err("1.3 postgres set satellite config")
}

func (satelliteRepo) Config(ctx context.Context, id fleet.SatelliteID) (fleet.Config, error) {
	return fleet.Config{}, notimpl.Err("1.3 postgres satellite config")
}

// SetIdentity runs UpsertSatelliteIdentity, with an empty entry list for
// nil because the column is NOT NULL.
func (satelliteRepo) SetIdentity(ctx context.Context, id fleet.SatelliteID, spiffeID string, entryIDs []string) error {
	return notimpl.Err("4.3 postgres set satellite identity")
}
