package postgres

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type configRepo struct{}

func (configRepo) Create(ctx context.Context, config fleet.Config) (fleet.Config, error) {
	return fleet.Config{}, notimpl.Err("1.3 postgres create config")
}

func (configRepo) Get(ctx context.Context, id fleet.ConfigID) (fleet.Config, error) {
	return fleet.Config{}, notimpl.Err("1.3 postgres get config")
}

func (configRepo) GetByName(ctx context.Context, name string) (fleet.Config, error) {
	return fleet.Config{}, notimpl.Err("1.3 postgres get config by name")
}

func (configRepo) List(ctx context.Context) ([]fleet.Config, error) {
	return nil, notimpl.Err("1.3 postgres list configs")
}

func (configRepo) Update(ctx context.Context, config fleet.Config) (fleet.Config, error) {
	return fleet.Config{}, notimpl.Err("1.3 postgres update config")
}

func (configRepo) Delete(ctx context.Context, id fleet.ConfigID) error {
	return notimpl.Err("1.3 postgres delete config")
}

func (configRepo) InUse(ctx context.Context, id fleet.ConfigID) (bool, error) {
	return false, notimpl.Err("1.3 postgres config in use")
}
