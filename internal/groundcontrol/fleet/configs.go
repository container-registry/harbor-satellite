package fleet

import (
	"context"
	"encoding/json"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type ConfigService struct{}

type CreateConfigCmd struct {
	Name     string
	Document json.RawMessage
}

// UpdateConfigCmd applies Patch as a JSON Merge Patch (RFC 7386).
type UpdateConfigCmd struct {
	Name  string
	Patch json.RawMessage
}

type AssignConfigCmd struct {
	Satellite, Config string
}

func (s *ConfigService) Create(ctx context.Context, cmd CreateConfigCmd) (Config, error) {
	return Config{}, notimpl.Err("1.2 create config")
}

func (s *ConfigService) Update(ctx context.Context, cmd UpdateConfigCmd) (Config, error) {
	return Config{}, notimpl.Err("1.2 update config")
}

// Delete fails with ErrInUse while a satellite uses the config.
func (s *ConfigService) Delete(ctx context.Context, name string) error {
	return notimpl.Err("1.2 delete config")
}

func (s *ConfigService) Get(ctx context.Context, name string) (Config, error) {
	return Config{}, notimpl.Err("1.2 get config")
}

func (s *ConfigService) List(ctx context.Context) ([]Config, error) {
	return nil, notimpl.Err("1.2 list configs")
}

func (s *ConfigService) Assign(ctx context.Context, cmd AssignConfigCmd) error {
	return notimpl.Err("1.2 assign config")
}
