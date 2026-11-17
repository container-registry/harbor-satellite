package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

type configRepo struct{ q *database.Queries }

func toConfig(c database.Config) fleet.Config {
	return fleet.Config{
		ID: fleet.ConfigID(c.ID), Name: c.ConfigName, RegistryURL: c.RegistryUrl, Document: c.Config,
		Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func (r configRepo) Create(ctx context.Context, config fleet.Config) (fleet.Config, error) {
	c, err := r.q.CreateFleetConfig(ctx, database.CreateFleetConfigParams{ConfigName: config.Name, RegistryUrl: config.RegistryURL, Config: config.Document})
	if err != nil {
		return fleet.Config{}, mapError(err)
	}
	return toConfig(c), nil
}

func (r configRepo) Get(ctx context.Context, id fleet.ConfigID) (fleet.Config, error) {
	c, err := r.q.GetFleetConfigByID(ctx, int32(id))
	if err != nil {
		return fleet.Config{}, mapError(err)
	}
	return toConfig(c), nil
}

func (r configRepo) GetByName(ctx context.Context, name string) (fleet.Config, error) {
	c, err := r.q.GetFleetConfigByName(ctx, name)
	if err != nil {
		return fleet.Config{}, mapError(err)
	}
	return toConfig(c), nil
}

func (r configRepo) List(ctx context.Context) ([]fleet.Config, error) {
	rows, err := r.q.ListFleetConfigs(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.Config, 0, len(rows))
	for _, c := range rows {
		out = append(out, toConfig(c))
	}
	return out, nil
}

// Update stores config if its Version is still the stored one; otherwise
// another update came first and it is fleet.ErrConflict (plan item 3.4).
func (r configRepo) Update(ctx context.Context, config fleet.Config) (fleet.Config, error) {
	c, err := r.q.UpdateFleetConfig(ctx, database.UpdateFleetConfigParams{
		ID: int32(config.ID), Version: config.Version, RegistryUrl: config.RegistryURL, Config: config.Document,
	})
	if err != nil {
		err = mapError(err)
		if errors.Is(err, fleet.ErrNotFound) {
			return fleet.Config{}, fmt.Errorf("config %s changed since it was read: %w", config.Name, fleet.ErrConflict)
		}
		return fleet.Config{}, err
	}
	return toConfig(c), nil
}

func (r configRepo) Delete(ctx context.Context, id fleet.ConfigID) error {
	return mapError(r.q.DeleteConfig(ctx, int32(id)))
}

func (r configRepo) InUse(ctx context.Context, id fleet.ConfigID) (bool, error) {
	inUse, err := r.q.ConfigInUse(ctx, int32(id))
	return inUse, mapError(err)
}
